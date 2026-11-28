package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Finish the synthetic read by cancelling with the existing preemption reason.
// No external upstream or second real connection is used.
type bridgeMeteredPreemptBody struct {
	prefix *strings.Reader
	cancel context.CancelCauseFunc
}

func (b *bridgeMeteredPreemptBody) Read(p []byte) (int, error) {
	if b.prefix.Len() > 0 {
		return b.prefix.Read(p)
	}
	b.cancel(errOpenAIWSSessionPreempted)
	return 0, context.Canceled
}
func (*bridgeMeteredPreemptBody) Close() error { return nil }

func TestOpenAIWSHTTPBridgePreemptedAfterMeteredResultCallsAfterTurnOnce(t *testing.T) {
	for _, metered := range []bool{true, false} {
		name := "without_usage"
		if metered {
			name = "with_usage"
		}
		t.Run(name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			timeoutCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			ctx, cancel := context.WithCancelCause(timeoutCtx)
			defer cancel(nil)

			const id = "resp_synthetic_preempt"
			prefix := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"" + id + "\",\"status\":\"in_progress\"}}\n\n" +
				"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ordinary partial answer\"}\n\n"
			if metered {
				prefix += "data: {\"type\":\"response.in_progress\",\"response\":{\"id\":\"" + id + "\",\"usage\":{\"input_tokens\":7,\"output_tokens\":2}}}\n\n"
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       &bridgeMeteredPreemptBody{prefix: strings.NewReader(prefix), cancel: cancel},
			}}
			cfg := &config.Config{}
			cfg.Gateway.OpenAIWS.Enabled = true
			cfg.Gateway.OpenAIWS.APIKeyEnabled = true
			cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
			cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
			cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
			cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), openaiWSStateStore: NewOpenAIWSStateStore(nil)}
			account := &Account{ID: 45, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1, Status: StatusActive, Schedulable: true,
				Extra: map[string]any{"openai_apikey_responses_websockets_v2_mode": OpenAIWSIngressModeHTTPBridge}}
			type observed struct {
				result *OpenAIForwardResult
				err    error
			}
			type outcome struct {
				err   error
				turns []observed
			}
			done := make(chan outcome, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := coderws.Accept(w, r, nil)
				if err != nil {
					done <- outcome{err: err}
					return
				}
				defer func() { _ = conn.CloseNow() }()
				_, first, err := conn.Read(timeoutCtx)
				if err != nil {
					done <- outcome{err: err}
					return
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = r.Clone(ctx)
				var turns []observed
				hooks := &OpenAIWSIngressHooks{ClientLifecycleContext: timeoutCtx, AfterTurn: func(_ int, result *OpenAIForwardResult, err error) {
					turns = append(turns, observed{result: result, err: err})
				}}
				err = svc.ProxyResponsesWebSocketFromClient(ctx, c, conn, account, "fixture-token", first, hooks)
				done <- outcome{err: err, turns: turns}
			}))
			defer server.Close()
			conn, _, err := coderws.Dial(timeoutCtx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
			require.NoError(t, err)
			defer func() { _ = conn.CloseNow() }()
			require.NoError(t, conn.Write(timeoutCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.6-sol","input":"hello"}`)))
			for {
				_, _, readErr := conn.Read(timeoutCtx)
				if readErr != nil {
					require.False(t, errors.Is(readErr, context.DeadlineExceeded))
					break
				}
			}
			select {
			case got := <-done:
				require.True(t, IsOpenAIWSSessionPreemptedError(got.err))
				if metered {
					require.Len(t, got.turns, 1)
					require.Error(t, got.turns[0].err)
					require.NotNil(t, got.turns[0].result)
					require.Equal(t, id, got.turns[0].result.RequestID)
					require.Equal(t, 7, got.turns[0].result.Usage.InputTokens)
					require.Equal(t, 2, got.turns[0].result.Usage.OutputTokens)
				} else {
					require.Empty(t, got.turns)
				}
			case <-timeoutCtx.Done():
				t.Fatal(timeoutCtx.Err())
			}
		})
	}
}

var _ io.ReadCloser = (*bridgeMeteredPreemptBody)(nil)
