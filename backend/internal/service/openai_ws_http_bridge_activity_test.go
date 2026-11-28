package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type bridgeActivityBody struct {
	*io.PipeReader
	closed chan struct{}
	once   sync.Once
	active atomic.Int32
	closes atomic.Int32
}

func (b *bridgeActivityBody) Read(p []byte) (int, error) {
	b.active.Add(1)
	defer b.active.Add(-1)
	return b.PipeReader.Read(p)
}
func (b *bridgeActivityBody) Close() error {
	b.closes.Add(1)
	b.once.Do(func() { close(b.closed); _ = b.PipeReader.Close() })
	return nil
}

const bridgeActivityID = "resp_activity_fixture"
const bridgeActivityCreated = "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_activity_fixture\",\"status\":\"in_progress\"}}\n\n"
const bridgeActivityDelta = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ordinary output\"}\n\n"
const bridgeActivityUsage = "data: {\"type\":\"response.in_progress\",\"response\":{\"id\":\"resp_activity_fixture\",\"usage\":{\"input_tokens\":7,\"output_tokens\":2}}}\n\n"
const bridgeActivityTerminal = "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_activity_fixture\",\"usage\":{\"input_tokens\":9,\"output_tokens\":3}}}\n\n"
const bridgeActivityKeepalive = "data: {\"type\":\"keepalive\"}\n\n"

func bridgeActivityWait(closed <-chan struct{}, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-closed:
		return false
	case <-timer.C:
		return true
	}
}
func bridgeActivityWrite(w *io.PipeWriter, s string) bool {
	_, err := io.WriteString(w, s)
	return err == nil
}

type bridgeActivityResult struct {
	result          *OpenAIForwardResult
	err             error
	events          [][]byte
	upstreamPayload []byte
	body            *bridgeActivityBody
}

func runBridgeActivityTurn(t *testing.T, ctx context.Context, cfg *config.Config, platform string, turn int, payload string,
	feed func(*io.PipeWriter, <-chan struct{}), onEvent func([]byte) error) bridgeActivityResult {
	t.Helper()
	gin.SetMode(gin.TestMode)
	pr, pw := io.Pipe()
	body := &bridgeActivityBody{PipeReader: pr, closed: make(chan struct{})}
	writerDone := make(chan struct{})
	var watchdogFired atomic.Bool
	watchdog := time.AfterFunc(4*time.Second, func() {
		watchdogFired.Store(true)
		_ = pw.CloseWithError(errors.New("synthetic fixture watchdog"))
		_ = pr.Close()
	})
	defer watchdog.Stop()
	go func() { defer close(writerDone); defer func() { _ = pw.Close() }(); feed(pw, body.closed) }()
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: body,
	}}
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
	account := &Account{ID: 95, Platform: platform, Type: AccountTypeAPIKey, Concurrency: 1, Credentials: map[string]any{"api_key": "fixture"}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil).WithContext(ctx)
	if payload == "" {
		payload = `{"type":"response.create","model":"gpt-5.6-sol","input":"hello"}`
	}
	got := bridgeActivityResult{body: body}
	got.result, got.err = svc.proxyOpenAIWSHTTPBridgeTurn(ctx, c, account, "fixture", []byte(payload), len(payload), gjson.Get(payload, "model").String(), "", "", "", "", turn, func(event []byte) error {
		got.events = append(got.events, append([]byte(nil), event...))
		if gjson.GetBytes(event, "status").Int() == 504 && gjson.GetBytes(event, "type").String() == "error" {
			select {
			case <-body.closed:
			default:
				t.Error("upstream must be closed before the final timeout frame")
			}
		}
		if onEvent != nil {
			return onEvent(event)
		}
		return nil
	})
	select {
	case <-writerDone:
	case <-time.After(time.Second):
		t.Fatal("fixture writer did not exit")
	}
	require.False(t, watchdogFired.Load(), "test watchdog cannot be the reason the turn finished")
	if strings.Contains(payload, `"type":"custom"`) {
		require.Eventually(t, func() bool { return body.active.Load() == 0 }, time.Second, 10*time.Millisecond, "tool transform source must exit")
	} else {
		require.Zero(t, body.active.Load(), "upstream Read must have exited before return")
	}
	require.Positive(t, body.closes.Load(), "response body closed")
	require.Len(t, upstream.requests, 1, "each fixture has a single upstream attempt")
	got.upstreamPayload = append([]byte(nil), upstream.lastBody...)
	return got
}

func (g bridgeActivityResult) has(want string) bool {
	for _, frame := range g.events {
		if gjson.GetBytes(frame, "type").String() == want {
			return true
		}
	}
	return false
}
func (g bridgeActivityResult) count(want string) int {
	n := 0
	for _, frame := range g.events {
		if gjson.GetBytes(frame, "type").String() == want {
			n++
		}
	}
	return n
}
func requireBridgeActivityTimeout(t *testing.T, got bridgeActivityResult, message string) {
	t.Helper()
	require.Error(t, got.err)
	var failover *UpstreamFailoverError
	require.False(t, errors.As(got.err, &failover), "new stream deadline must not replay generation")
	var closeErr *OpenAIWSClientCloseError
	require.ErrorAs(t, got.err, &closeErr)
	require.Equal(t, coderws.StatusInternalError, closeErr.StatusCode())
	require.NotNil(t, got.result)
	require.False(t, got.has("response.completed"))
	require.Equal(t, 1, got.count("error"))
	for _, event := range got.events {
		if gjson.GetBytes(event, "type").String() == "error" {
			require.Equal(t, int64(504), gjson.GetBytes(event, "status").Int())
			require.Contains(t, gjson.GetBytes(event, "error.message").String(), message)
		}
	}
}

func TestProxyOpenAIWSHTTPBridgeTurn_ActiveStreamTimeout(t *testing.T) {
	t.Run("idle_after_output", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Gateway.StreamDataIntervalTimeout = 1
		consumed := make(chan struct{})
		var once sync.Once
		got := runBridgeActivityTurn(t, context.Background(), cfg, PlatformOpenAI, 1, "", func(w *io.PipeWriter, closed <-chan struct{}) {
			if !bridgeActivityWrite(w, bridgeActivityCreated+bridgeActivityDelta+bridgeActivityUsage) {
				return
			}
			select {
			case <-consumed:
			case <-closed:
				return
			case <-time.After(2 * time.Second):
				return
			}
			if bridgeActivityWait(closed, 2500*time.Millisecond) {
				bridgeActivityWrite(w, bridgeActivityTerminal)
			}
		}, func(event []byte) error {
			if gjson.GetBytes(event, "type").String() == "response.in_progress" {
				once.Do(func() { close(consumed) })
			}
			return nil
		})
		requireBridgeActivityTimeout(t, got, "no upstream data")
		require.Equal(t, bridgeActivityID, got.result.RequestID)
		require.Equal(t, 7, got.result.Usage.InputTokens)
		require.Equal(t, 2, got.result.Usage.OutputTokens)
	})
	t.Run("first_output_with_heartbeats", func(t *testing.T) {
		for _, turn := range []int{1, 2} {
			t.Run(fmt.Sprint(turn), func(t *testing.T) {
				cfg := &config.Config{}
				cfg.Gateway.StreamDataIntervalTimeout = 3
				cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 1
				got := runBridgeActivityTurn(t, context.Background(), cfg, PlatformOpenAI, turn, "", func(w *io.PipeWriter, closed <-chan struct{}) {
					if !bridgeActivityWrite(w, bridgeActivityCreated+"data: {\"type\":\"response.relay_ready\"}\n\n") {
						return
					}
					for i := 0; i < 10; i++ {
						if !bridgeActivityWait(closed, 250*time.Millisecond) || !bridgeActivityWrite(w, bridgeActivityKeepalive) {
							return
						}
					}
					bridgeActivityWrite(w, bridgeActivityDelta+bridgeActivityTerminal)
				}, nil)
				requireBridgeActivityTimeout(t, got, "no model output")
				require.False(t, got.has("response.output_text.delta"))
				if turn == 1 {
					require.False(t, got.has("response.relay_ready"), "nonsemantic metadata must stay staged")
				}
			})
		}
	})
}

func TestProxyOpenAIWSHTTPBridgeTurn_BoundedDisconnectDrain(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.StreamDataIntervalTimeout = 1
	disconnected := false
	got := runBridgeActivityTurn(t, context.Background(), cfg, PlatformOpenAI, 1, "", func(w *io.PipeWriter, closed <-chan struct{}) {
		if !bridgeActivityWrite(w, bridgeActivityCreated+bridgeActivityDelta+bridgeActivityUsage+bridgeActivityKeepalive) {
			return
		}
		for i := 0; i < 12; i++ {
			if !bridgeActivityWait(closed, 200*time.Millisecond) || !bridgeActivityWrite(w, ": live\n\n") {
				return
			}
		}
		bridgeActivityWrite(w, bridgeActivityTerminal)
	}, func(event []byte) error {
		if disconnected {
			t.Error("writer called after disconnect")
		}
		if gjson.GetBytes(event, "type").String() == "keepalive" {
			disconnected = true
			return io.EOF
		}
		return nil
	})
	require.True(t, disconnected)
	require.Error(t, got.err)
	var closeErr *OpenAIWSClientCloseError
	require.ErrorAs(t, got.err, &closeErr)
	require.Equal(t, coderws.StatusNormalClosure, closeErr.StatusCode())
	var failover *UpstreamFailoverError
	require.False(t, errors.As(got.err, &failover))
	require.NotNil(t, got.result)
	require.Equal(t, 7, got.result.Usage.InputTokens)
	require.Equal(t, 2, got.result.Usage.OutputTokens)
	require.False(t, got.has("response.completed"))
}

func TestProxyOpenAIWSHTTPBridgeTurn_ActiveKeepalive(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.StreamDataIntervalTimeout = 3
	cfg.Gateway.StreamKeepaliveInterval = 1
	heartbeat := make(chan struct{})
	var once sync.Once
	got := runBridgeActivityTurn(t, context.Background(), cfg, PlatformOpenAI, 1, "", func(w *io.PipeWriter, closed <-chan struct{}) {
		if !bridgeActivityWrite(w, bridgeActivityCreated) {
			return
		}
		select {
		case <-heartbeat:
		case <-time.After(2500 * time.Millisecond):
		case <-closed:
			return
		}
		bridgeActivityWrite(w, bridgeActivityTerminal)
	}, func(event []byte) error {
		if gjson.GetBytes(event, "type").String() == "keepalive" {
			once.Do(func() { close(heartbeat) })
		}
		return nil
	})
	require.NoError(t, got.err)
	require.NotNil(t, got.result)
	require.Equal(t, 9, got.result.Usage.InputTokens)
	require.Len(t, got.events, 3)
	require.JSONEq(t, `{"type":"keepalive"}`, string(got.events[0]))
	require.Equal(t, "response.created", gjson.GetBytes(got.events[1], "type").String())
	require.Equal(t, "response.completed", gjson.GetBytes(got.events[2], "type").String())
	require.Nil(t, got.result.FirstTokenMs, "synthetic keepalive is not model output")
}

func TestProxyOpenAIWSHTTPBridgeTurn_ActivityProgressAndDisabledTimers(t *testing.T) {
	for _, tc := range []struct {
		name              string
		idle, first, high int
		payload           string
		feed              func(*io.PipeWriter, <-chan struct{})
	}{
		{"progress", 1, 1, 0, "", func(w *io.PipeWriter, closed <-chan struct{}) {
			if !bridgeActivityWrite(w, bridgeActivityCreated+bridgeActivityDelta) {
				return
			}
			for i := 0; i < 4; i++ {
				if !bridgeActivityWait(closed, 350*time.Millisecond) || !bridgeActivityWrite(w, ": comment\n\n") {
					return
				}
			}
			bridgeActivityWrite(w, bridgeActivityTerminal)
		}},
		{"disabled", 0, 0, 0, "", func(w *io.PipeWriter, closed <-chan struct{}) {
			if bridgeActivityWrite(w, bridgeActivityCreated) && bridgeActivityWait(closed, 1200*time.Millisecond) {
				bridgeActivityWrite(w, bridgeActivityTerminal)
			}
		}},
		{"high_effort", 3, 1, 3, `{"type":"response.create","model":"gpt-5.6-sol","reasoning":{"effort":"high"},"input":"hello"}`, func(w *io.PipeWriter, closed <-chan struct{}) {
			if bridgeActivityWrite(w, bridgeActivityCreated) && bridgeActivityWait(closed, 1500*time.Millisecond) {
				bridgeActivityWrite(w, bridgeActivityDelta+bridgeActivityTerminal)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Gateway.StreamDataIntervalTimeout = tc.idle
			cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = tc.first
			cfg.Gateway.OpenAIHighEffortFirstOutputTimeoutSeconds = tc.high
			got := runBridgeActivityTurn(t, context.Background(), cfg, PlatformOpenAI, 1, tc.payload, tc.feed, nil)
			require.NoError(t, got.err)
			require.NotNil(t, got.result)
			require.Equal(t, 9, got.result.Usage.InputTokens)
			require.True(t, got.has("response.completed"))
			require.Zero(t, got.count("keepalive"))
			require.Equal(t, int32(1), got.body.closes.Load())
		})
	}
}
func TestProxyOpenAIWSHTTPBridgeTurn_GrokActivityTimeout(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.StreamDataIntervalTimeout = 1
	cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 1
	got := runBridgeActivityTurn(t, context.Background(), cfg, PlatformGrok, 1, `{"type":"response.create","model":"grok-4.3","input":"hello"}`, func(w *io.PipeWriter, closed <-chan struct{}) {
		if bridgeActivityWrite(w, bridgeActivityCreated+bridgeActivityDelta+bridgeActivityUsage) && bridgeActivityWait(closed, 2500*time.Millisecond) {
			bridgeActivityWrite(w, bridgeActivityTerminal)
		}
	}, nil)
	requireBridgeActivityTimeout(t, got, "no upstream data")
	require.Equal(t, 7, got.result.Usage.InputTokens)
	require.Equal(t, defaultGrokStreamIdleTimeout, resolveGrokStreamIdleTimeout(0))
}
func TestProxyOpenAIWSHTTPBridgeTurn_KeepaliveRecentWriteSuppression(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.StreamDataIntervalTimeout = 3
	cfg.Gateway.StreamKeepaliveInterval = 1
	got := runBridgeActivityTurn(t, context.Background(), cfg, PlatformOpenAI, 1, "", func(w *io.PipeWriter, closed <-chan struct{}) {
		if !bridgeActivityWrite(w, bridgeActivityCreated+bridgeActivityDelta) {
			return
		}
		if !bridgeActivityWait(closed, 600*time.Millisecond) || !bridgeActivityWrite(w, bridgeActivityKeepalive) {
			return
		}
		if bridgeActivityWait(closed, 600*time.Millisecond) {
			bridgeActivityWrite(w, bridgeActivityTerminal)
		}
	}, nil)
	require.NoError(t, got.err)
	require.Equal(t, 1, got.count("keepalive"), "only the actual upstream keepalive")
	require.True(t, got.has("response.completed"))
}
func TestProxyOpenAIWSHTTPBridgeTurn_DrainCompletesBeforeBudget(t *testing.T) {
	for _, ordinaryCancel := range []bool{false, true} {
		t.Run(fmt.Sprint(ordinaryCancel), func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Gateway.StreamDataIntervalTimeout = 1
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			disconnected := false
			got := runBridgeActivityTurn(t, ctx, cfg, PlatformOpenAI, 1, "", func(w *io.PipeWriter, closed <-chan struct{}) {
				if !bridgeActivityWrite(w, bridgeActivityCreated+bridgeActivityDelta+bridgeActivityUsage+bridgeActivityKeepalive) {
					return
				}
				if bridgeActivityWait(closed, 300*time.Millisecond) {
					bridgeActivityWrite(w, bridgeActivityTerminal)
				}
			}, func(event []byte) error {
				if disconnected {
					t.Error("client write after cancellation")
				}
				if gjson.GetBytes(event, "type").String() == "keepalive" {
					disconnected = true
					if ordinaryCancel {
						cancel()
						return nil
					}
					return io.EOF
				}
				return nil
			})
			require.True(t, disconnected)
			require.NoError(t, got.err)
			require.NotNil(t, got.result)
			require.Equal(t, 9, got.result.Usage.InputTokens)
			require.Equal(t, 3, got.result.Usage.OutputTokens)
			require.False(t, got.has("response.completed"))
			require.Equal(t, int32(1), got.body.closes.Load())
		})
	}
}
func TestProxyOpenAIWSHTTPBridgeTurn_HardControlClosesReader(t *testing.T) {
	for _, cause := range []error{ErrOpenAIWSIngressLeaseLost, errOpenAIWSSessionPreempted} {
		t.Run(cause.Error(), func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Gateway.StreamDataIntervalTimeout = 3
			cfg.Gateway.StreamKeepaliveInterval = 1
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(nil)
			got := runBridgeActivityTurn(t, ctx, cfg, PlatformOpenAI, 1, "", func(w *io.PipeWriter, closed <-chan struct{}) {
				if bridgeActivityWrite(w, bridgeActivityCreated+bridgeActivityDelta+bridgeActivityUsage) && bridgeActivityWait(closed, 2500*time.Millisecond) {
					bridgeActivityWrite(w, bridgeActivityTerminal)
				}
			}, func(event []byte) error {
				if gjson.GetBytes(event, "type").String() == "response.in_progress" {
					cancel(cause)
				}
				return nil
			})
			require.ErrorIs(t, got.err, cause)
			require.NotNil(t, got.result)
			require.Equal(t, 7, got.result.Usage.InputTokens)
			require.False(t, got.has("response.completed"))
			require.False(t, got.has("error"))
			require.Equal(t, int32(1), got.body.closes.Load())
		})
	}
}
func TestProxyOpenAIWSHTTPBridgeTurn_DrainNoSecondCancellationSignal(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.StreamDataIntervalTimeout = 1
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &openAIWSSessionPreemptState{}
	ctx = context.WithValue(ctx, openAIWSSessionPreemptContextKey{}, state)
	cancelled := make(chan struct{})
	got := runBridgeActivityTurn(t, ctx, cfg, PlatformOpenAI, 1, "", func(w *io.PipeWriter, closed <-chan struct{}) {
		if !bridgeActivityWrite(w, bridgeActivityCreated+bridgeActivityDelta+bridgeActivityUsage) {
			return
		}
		select {
		case <-cancelled:
		case <-closed:
			return
		}
		if !bridgeActivityWait(closed, 200*time.Millisecond) {
			return
		}
		state.preempted.Store(true)
		if bridgeActivityWait(closed, 2200*time.Millisecond) {
			bridgeActivityWrite(w, bridgeActivityTerminal)
		}
	}, func(event []byte) error {
		if gjson.GetBytes(event, "type").String() == "response.in_progress" {
			cancel()
			close(cancelled)
		}
		return nil
	})
	require.Error(t, got.err)
	require.True(t, errors.Is(got.err, context.Canceled) || IsOpenAIWSSessionPreemptedError(got.err))
	require.NotNil(t, got.result)
	require.Equal(t, 7, got.result.Usage.InputTokens)
	require.False(t, got.has("response.completed"))
	require.False(t, got.has("error"))
}
func TestProxyOpenAIWSHTTPBridgeTurn_SyntheticKeepaliveWriteFailure(t *testing.T) {
	for _, writeErr := range []error{io.EOF, errors.New("synthetic writer failure")} {
		t.Run(writeErr.Error(), func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Gateway.StreamDataIntervalTimeout = 3
			cfg.Gateway.StreamKeepaliveInterval = 1
			wrote := make(chan struct{})
			var once sync.Once
			got := runBridgeActivityTurn(t, context.Background(), cfg, PlatformOpenAI, 1, "", func(w *io.PipeWriter, closed <-chan struct{}) {
				if !bridgeActivityWrite(w, bridgeActivityCreated+bridgeActivityUsage) {
					return
				}
				select {
				case <-wrote:
				case <-closed:
					return
				case <-time.After(2500 * time.Millisecond):
				}
				if bridgeActivityWait(closed, 100*time.Millisecond) {
					bridgeActivityWrite(w, bridgeActivityTerminal)
				}
			}, func(event []byte) error {
				if gjson.GetBytes(event, "type").String() == "keepalive" {
					once.Do(func() { close(wrote) })
					return writeErr
				}
				return nil
			})
			require.NotNil(t, got.result)
			require.Len(t, got.events, 1)
			require.True(t, got.has("keepalive"))
			if writeErr == io.EOF {
				require.NoError(t, got.err)
				require.Equal(t, 9, got.result.Usage.InputTokens)
			} else {
				require.ErrorIs(t, got.err, writeErr)
				require.Equal(t, 7, got.result.Usage.InputTokens)
				var err *openAIWSIngressTurnError
				require.ErrorAs(t, got.err, &err)
				require.Equal(t, "write_client", err.stage)
				require.False(t, isOpenAIWSIngressTurnRetryable(got.err))
			}
		})
	}
}
func TestProxyOpenAIWSHTTPBridgeTurn_DeadlinePreservesPendingBareError(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.StreamDataIntervalTimeout = 1
	got := runBridgeActivityTurn(t, context.Background(), cfg, PlatformOpenAI, 1, "", func(w *io.PipeWriter, closed <-chan struct{}) {
		if bridgeActivityWrite(w, bridgeActivityCreated+bridgeActivityDelta+bridgeActivityUsage+"data: {\"type\":\"error\",\"error\":{\"code\":\"server_error\",\"message\":\"synthetic upstream error\"}}\n\n") && bridgeActivityWait(closed, 2500*time.Millisecond) {
			bridgeActivityWrite(w, bridgeActivityTerminal)
		}
	}, nil)
	require.Error(t, got.err)
	require.NotNil(t, got.result)
	require.Equal(t, 7, got.result.Usage.InputTokens)
	require.Equal(t, 1, got.count("response.failed"))
	require.Zero(t, got.count("error"))
	require.False(t, got.has("response.completed"))
	var failover *UpstreamFailoverError
	require.False(t, errors.As(got.err, &failover))
}

func TestProxyOpenAIWSHTTPBridgeTurn_GrokDoesNotUseOpenAIFirstOutputDeadline(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.StreamDataIntervalTimeout = 3
	cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 1
	got := runBridgeActivityTurn(t, context.Background(), cfg, PlatformGrok, 1, `{"type":"response.create","model":"grok-4.3","input":"hello"}`, func(w *io.PipeWriter, closed <-chan struct{}) {
		if bridgeActivityWrite(w, bridgeActivityCreated) && bridgeActivityWait(closed, 1500*time.Millisecond) {
			bridgeActivityWrite(w, bridgeActivityDelta+bridgeActivityTerminal)
		}
	}, nil)
	require.NoError(t, got.err)
	require.NotNil(t, got.result)
	require.Equal(t, 9, got.result.Usage.InputTokens)
}

func TestProxyOpenAIWSHTTPBridgeTurn_ActivityClosesMappedToolSource(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.StreamDataIntervalTimeout = 1
	payload := `{"type":"response.create","model":"gpt-5.6-sol","tools":[{"type":"custom","name":"lookup","description":"Look up a word"}],"input":"hello"}`
	got := runBridgeActivityTurn(t, context.Background(), cfg, PlatformOpenAI, 1, payload, func(w *io.PipeWriter, closed <-chan struct{}) {
		if bridgeActivityWrite(w, bridgeActivityCreated+bridgeActivityDelta+bridgeActivityUsage) && bridgeActivityWait(closed, 2500*time.Millisecond) {
			bridgeActivityWrite(w, bridgeActivityTerminal)
		}
	}, nil)
	requireBridgeActivityTimeout(t, got, "no upstream data")
	require.Equal(t, 7, got.result.Usage.InputTokens)
	require.Equal(t, "function", gjson.GetBytes(got.upstreamPayload, "tools.0.type").String(), "fixture must exercise the existing tool lowering/response wrapper")
}

func TestProxyOpenAIWSHTTPBridgeTurn_ActivityClosesPluginResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pr, pw := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var cancels, finishes atomic.Int32
	body := &pluginResponseBody{reader: pr, cancel: func() { cancels.Add(1); cancel() }, done: func() { finishes.Add(1) }}
	writerDone := make(chan struct{})
	watchdog := time.AfterFunc(4*time.Second, func() { _ = pw.Close(); _ = pr.Close() })
	defer watchdog.Stop()
	go func() {
		defer close(writerDone)
		defer func() { _ = pw.Close() }()
		if bridgeActivityWrite(pw, bridgeActivityCreated+bridgeActivityDelta+bridgeActivityUsage) && bridgeActivityWait(ctx.Done(), 2500*time.Millisecond) {
			bridgeActivityWrite(pw, bridgeActivityTerminal)
		}
	}()
	cfg := &config.Config{}
	cfg.Gateway.StreamDataIntervalTimeout = 1
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: body}}
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
	account := &Account{ID: 91, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	payload := []byte(`{"type":"response.create","model":"gpt-5.6-sol","input":"hello"}`)
	got := bridgeActivityResult{}
	got.result, got.err = svc.proxyOpenAIWSHTTPBridgeTurn(context.Background(), c, account, "fixture", payload, len(payload), "gpt-5.6-sol", "", "", "", "", 1, func(event []byte) error {
		got.events = append(got.events, append([]byte(nil), event...))
		return nil
	})
	requireBridgeActivityTimeout(t, got, "no upstream data")
	require.Equal(t, int32(1), cancels.Load())
	require.Equal(t, int32(1), finishes.Load())
	select {
	case <-writerDone:
	case <-time.After(time.Second):
		t.Fatal("plugin writer did not exit")
	}
}

func TestOpenAIWSHTTPBridgeActiveTimeoutCallsAfterTurnOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pr, pw := io.Pipe()
	body := &bridgeActivityBody{PipeReader: pr, closed: make(chan struct{})}
	writerDone := make(chan struct{})
	watchdog := time.AfterFunc(4*time.Second, func() { _ = pw.Close(); _ = pr.Close() })
	defer watchdog.Stop()
	go func() {
		defer close(writerDone)
		defer func() { _ = pw.Close() }()
		if bridgeActivityWrite(pw, bridgeActivityCreated+bridgeActivityDelta+bridgeActivityUsage) && bridgeActivityWait(body.closed, 2500*time.Millisecond) {
			bridgeActivityWrite(pw, bridgeActivityTerminal)
		}
	}()
	cfg := &config.Config{}
	cfg.Gateway.StreamDataIntervalTimeout = 1
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: body}}
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), openaiWSStateStore: NewOpenAIWSStateStore(nil)}
	account := &Account{ID: 92, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1,
		Extra: map[string]any{"openai_apikey_responses_websockets_v2_mode": OpenAIWSIngressModeHTTPBridge}}
	type hookOutcome struct {
		err, turnErr error
		result       *OpenAIForwardResult
		calls        int
	}
	done := make(chan hookOutcome, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			done <- hookOutcome{err: err}
			return
		}
		defer func() { _ = conn.CloseNow() }()
		_, first, err := conn.Read(ctx)
		if err != nil {
			done <- hookOutcome{err: err}
			return
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = r.Clone(ctx)
		got := hookOutcome{}
		hooks := &OpenAIWSIngressHooks{ClientLifecycleContext: ctx, AfterTurn: func(_ int, result *OpenAIForwardResult, err error) {
			got.calls++
			got.result = result
			got.turnErr = err
		}}
		got.err = svc.ProxyResponsesWebSocketFromClient(ctx, c, conn, account, "fixture-token", first, hooks)
		done <- got
	}))
	defer server.Close()
	conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer func() { _ = conn.CloseNow() }()
	require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.6-sol","input":"hello"}`)))
	errorsSeen := 0
	for {
		_, event, readErr := conn.Read(ctx)
		if readErr != nil {
			require.False(t, errors.Is(readErr, context.DeadlineExceeded))
			break
		}
		if gjson.GetBytes(event, "type").String() == "error" {
			errorsSeen++
		}
	}
	select {
	case got := <-done:
		require.Error(t, got.err)
		require.Error(t, got.turnErr)
		require.Equal(t, 1, got.calls)
		require.NotNil(t, got.result)
		require.Equal(t, 7, got.result.Usage.InputTokens)
		require.Equal(t, bridgeActivityID, got.result.RequestID)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.Equal(t, 1, errorsSeen)
	require.Equal(t, int32(1), body.closes.Load())
	require.Zero(t, body.active.Load())
	select {
	case <-writerDone:
	case <-time.After(time.Second):
		t.Fatal("fixture writer did not exit")
	}
}

type bridgeActivityDelayedUpstream struct{ *httpUpstreamRecorder }

func (u *bridgeActivityDelayedUpstream) Do(req *http.Request, proxy string, account int64, concurrency int) (*http.Response, error) {
	time.Sleep(1200 * time.Millisecond)
	return u.httpUpstreamRecorder.Do(req, proxy, account, concurrency)
}
func TestProxyOpenAIWSHTTPBridgeTurn_FirstOutputDeadlineAfterHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 1
	upstream := &bridgeActivityDelayedUpstream{httpUpstreamRecorder: &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(bridgeActivityDelta + bridgeActivityTerminal)),
	}}}
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
	account := &Account{ID: 93, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	payload := []byte(`{"type":"response.create","model":"gpt-5.6-sol","input":"hello"}`)
	got := bridgeActivityResult{}
	got.result, got.err = svc.proxyOpenAIWSHTTPBridgeTurn(context.Background(), c, account, "fixture", payload, len(payload), "gpt-5.6-sol", "", "", "", "", 1, func(event []byte) error { got.events = append(got.events, append([]byte(nil), event...)); return nil })
	requireBridgeActivityTimeout(t, got, "no model output")
	require.False(t, got.has("response.output_text.delta"))
	require.Len(t, upstream.requests, 1)
}

func TestProxyOpenAIWSHTTPBridgeTurn_TerminalClosesWithoutWaitingForEOF(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.StreamDataIntervalTimeout = 1
	var closedBeforeEOF atomic.Bool
	got := runBridgeActivityTurn(t, context.Background(), cfg, PlatformOpenAI, 1, "", func(w *io.PipeWriter, closed <-chan struct{}) {
		if !bridgeActivityWrite(w, bridgeActivityCreated+bridgeActivityDelta+bridgeActivityTerminal) {
			return
		}
		select {
		case <-closed:
			closedBeforeEOF.Store(true)
		case <-time.After(2500 * time.Millisecond):
		}
	}, nil)
	require.NoError(t, got.err)
	require.True(t, closedBeforeEOF.Load())
	require.True(t, got.has("response.completed"))
	require.Equal(t, int32(1), got.body.closes.Load())
}
func TestProxyOpenAIWSHTTPBridgeTurn_DeadlineAfterBoundedWriterReleasesPendingReader(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.StreamDataIntervalTimeout = 1
	got := runBridgeActivityTurn(t, context.Background(), cfg, PlatformOpenAI, 1, "", func(w *io.PipeWriter, closed <-chan struct{}) {
		if bridgeActivityWrite(w, bridgeActivityCreated+bridgeActivityDelta+bridgeActivityUsage) && bridgeActivityWait(closed, 2500*time.Millisecond) {
			bridgeActivityWrite(w, bridgeActivityTerminal)
		}
	}, func(event []byte) error {
		if gjson.GetBytes(event, "type").String() == "response.output_text.delta" {
			time.Sleep(1200 * time.Millisecond)
		}
		return nil
	})
	requireBridgeActivityTimeout(t, got, "no upstream data")
	require.Equal(t, int32(1), got.body.closes.Load())
	require.Zero(t, got.body.active.Load())
}

func TestProxyOpenAIWSHTTPBridgeTurn_OrdinaryCancellationWinsGeneralWriteFailure(t *testing.T) {
	for _, bare := range []bool{false, true} {
		t.Run(fmt.Sprint(bare), func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Gateway.StreamDataIntervalTimeout = 1
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sawCancellation := false
			got := runBridgeActivityTurn(t, ctx, cfg, PlatformOpenAI, 1, "", func(w *io.PipeWriter, closed <-chan struct{}) {
				prefix := bridgeActivityCreated + bridgeActivityDelta + bridgeActivityUsage
				if bare {
					bridgeActivityWrite(w, prefix+"data: {\"type\":\"error\",\"error\":{\"code\":\"server_error\",\"message\":\"synthetic upstream error\"}}\n\n")
					return
				}
				if bridgeActivityWrite(w, prefix) && bridgeActivityWait(closed, 300*time.Millisecond) {
					bridgeActivityWrite(w, bridgeActivityTerminal)
				}
			}, func(event []byte) error {
				if sawCancellation {
					t.Error("unexpected write after ordinary cancellation")
				}
				eventType := gjson.GetBytes(event, "type").String()
				if (!bare && eventType == "response.in_progress") || (bare && eventType == "response.failed") {
					sawCancellation = true
					cancel()
					// A generic write failure is not itself an EOF/disconnect marker.
					return context.DeadlineExceeded
				}
				return nil
			})
			require.True(t, sawCancellation)
			require.NotNil(t, got.result)
			require.False(t, got.has("response.completed"))
			if bare {
				require.ErrorIs(t, got.err, context.Canceled)
				var closeErr *OpenAIWSClientCloseError
				require.ErrorAs(t, got.err, &closeErr)
				require.Equal(t, coderws.StatusNormalClosure, closeErr.StatusCode())
				require.Equal(t, 7, got.result.Usage.InputTokens)
				require.Equal(t, 2, got.result.Usage.OutputTokens)
			} else {
				require.NoError(t, got.err)
				require.Equal(t, 9, got.result.Usage.InputTokens)
				require.Equal(t, 3, got.result.Usage.OutputTokens)
			}
			require.Equal(t, int32(1), got.body.closes.Load())
		})
	}
}
