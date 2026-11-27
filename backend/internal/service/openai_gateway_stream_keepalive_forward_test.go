package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const (
	relayReadyFrame        = "event: response.relay_ready\ndata: {\"type\":\"response.relay_ready\"}\n\n"
	recoveryCreatedFrame   = "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_attempt\"}}\n\n"
	recoveryFailedFrame    = "event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_attempt\",\"error\":{\"code\":\"server_error\",\"message\":\"upstream processing failed\"}}}\n\n"
	recoveryBareErrorFrame = "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"rate_limit_error\",\"code\":\"rate_limit_exceeded\",\"message\":\"slow down\"}}\n\n"
	recoveryTextFrame      = "data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n\n"
	recoveryCompletedFrame = "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_attempt\",\"usage\":{\"input_tokens\":3,\"output_tokens\":4}}}\n\n"
	queueHeartbeatFrame    = ":\n\n"
)

func recoveryHTTPResponse(body io.ReadCloser) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: body}
}

func runRecoveryHTTPStream(s *OpenAIGatewayService, c *gin.Context, account *Account, body io.ReadCloser, passthrough bool) (*int, error) {
	if passthrough {
		result, err := s.handleStreamingResponsePassthrough(c.Request.Context(), recoveryHTTPResponse(body), c, account, time.Now(), "model", "model")
		if result == nil {
			return nil, err
		}
		return result.firstTokenMs, err
	}
	result, err := s.handleStreamingResponse(c.Request.Context(), recoveryHTTPResponse(body), c, account, time.Now(), "model", "model")
	if result == nil {
		return nil, err
	}
	return result.firstTokenMs, err
}

func writeRecoveryQueueHeartbeat(t *testing.T, c *gin.Context) {
	t.Helper()
	c.Header("Content-Type", "text/event-stream")
	n, err := c.Writer.WriteString(queueHeartbeatFrame)
	require.NoError(t, err)
	c.Set(GatewayStreamHeartbeatBytesKey, n)
}

func TestOpenAIStreamRelayReadyClassification(t *testing.T) {
	const event = "response.relay_ready"
	account := &Account{Platform: PlatformOpenAI}
	for _, tc := range []struct {
		name, data, effective, pending string
		want                           bool
	}{
		{"data_only", `{"type":"response.relay_ready"}`, event, "", true},
		{"matching_event", ` { "type" : "response.relay_ready" } `, event, event, true},
		{"extra", `{"type":"response.relay_ready","content":"anything"}`, event, event, false},
		{"duplicate", `{"type":"response.relay_ready","type":"response.relay_ready"}`, event, event, false},
		{"missing", `{}`, event, event, false},
		{"wrong_type", `{"type":"other"}`, event, event, false},
		{"non_string_type", `{"type":123}`, event, event, false},
		{"invalid", `{"type":"response.relay_ready",}`, event, event, false},
		{"not_object", `[{"type":"response.relay_ready"}]`, event, event, false},
		{"conflicting_event", `{"type":"response.relay_ready"}`, event, "other", false},
		{"wrong_effective_type", `{"type":"response.relay_ready"}`, "other", event, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, isOpenAIHTTPRelayReadyMetadata(account, tc.data, tc.effective, tc.pending))
		})
	}
	data := `{"type":"response.relay_ready"}`
	require.False(t, isOpenAIHTTPRelayReadyMetadata(&Account{Platform: PlatformGrok}, data, event, event))
	require.False(t, isOpenAIHTTPRelayReadyMetadata(nil, data, event, event))
	// Shared classification (including independent WebSocket callers) is unchanged.
	require.True(t, openAIStreamDataStartsClientOutput(data, event))
	require.True(t, openAIStreamDataStartsTTFT(data, event, false, OpenAITTFTModeSemantic))
	require.False(t, openAIStreamDataStartsTTFT(data, event, false, OpenAITTFTModeVisible))
}

func TestOpenAIStreamingRelayReadyBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prior := gatewayForwardingCache.Load()
	gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{openAITTFTMode: OpenAITTFTModeSemantic})
	t.Cleanup(func() {
		if prior != nil {
			gatewayForwardingCache.Store(prior)
		} else {
			gatewayForwardingCache.Store((*cachedGatewayForwardingSettings)(nil))
		}
	})
	for _, passthrough := range []bool{false, true} {
		name := "native"
		if passthrough {
			name = "passthrough"
		}
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct {
				name, prefix, tail, platform string
				failover                     bool
			}{
				{"empty_relay", relayReadyFrame + recoveryCreatedFrame, recoveryFailedFrame, PlatformOpenAI, true},
				{"bare_error", relayReadyFrame + recoveryCreatedFrame, recoveryBareErrorFrame, PlatformOpenAI, true},
				{"extra_member", "event: response.relay_ready\ndata: {\"type\":\"response.relay_ready\",\"sequence_number\":0}\n\n", recoveryFailedFrame, PlatformOpenAI, false},
				{"duplicate_type", "data: {\"type\":\"response.relay_ready\",\"type\":\"response.relay_ready\"}\n\n", recoveryFailedFrame, PlatformOpenAI, false},
				{"missing_type", "event: response.relay_ready\ndata: {}\n\n", recoveryFailedFrame, PlatformOpenAI, false},
				{"different_type", "event: response.relay_ready\ndata: {\"type\":\"unknown.event\"}\n\n", recoveryFailedFrame, PlatformOpenAI, false},
				{"invalid_json", "event: response.relay_ready\ndata: {\"type\":\"response.relay_ready\",}\n\n", recoveryFailedFrame, PlatformOpenAI, false},
				{"conflicting_event_line", "event: unknown.event\ndata: {\"type\":\"response.relay_ready\"}\n\n", recoveryFailedFrame, PlatformOpenAI, false},
				{"unknown_event", "data: {\"type\":\"unknown.event\"}\n\n", recoveryFailedFrame, PlatformOpenAI, false},
				{"after_text", relayReadyFrame + recoveryCreatedFrame + recoveryTextFrame, recoveryFailedFrame, PlatformOpenAI, false},
				{"after_reasoning", relayReadyFrame + recoveryCreatedFrame + "data: {\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\"reasoning\"}\n\n", recoveryFailedFrame, PlatformOpenAI, false},
				{"after_tool", relayReadyFrame + recoveryCreatedFrame + "data: {\"type\":\"response.function_call_arguments.delta\",\"delta\":\"{}\"}\n\n", recoveryFailedFrame, PlatformOpenAI, false},
				{"encrypted_content", relayReadyFrame, "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"type\":\"invalid_request_error\",\"code\":\"invalid_encrypted_content\",\"message\":\"invalid encrypted content\"}}}\n\n", PlatformOpenAI, false},
				{"empty_completed", relayReadyFrame, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_attempt\",\"output\":[]}}\n\n", PlatformOpenAI, true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					c, rec := newKeepaliveForwardContext()
					svc := newKeepaliveForwardService(0)
					ttft, err := runRecoveryHTTPStream(svc, c, &Account{ID: 1, Platform: tc.platform}, io.NopCloser(strings.NewReader(tc.prefix+tc.tail)), passthrough)
					var failover *UpstreamFailoverError
					require.Error(t, err)
					require.Equal(t, tc.failover, errors.As(err, &failover))
					if tc.failover {
						require.Empty(t, rec.Body.String())
						require.Nil(t, ttft)
					} else {
						require.NotEmpty(t, rec.Body.String())
					}
				})
			}
			t.Run("non_openai_after_relay_delivery", func(t *testing.T) {
				rec := newOpenAIResponseFlushRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				release := make(chan struct{})
				var once sync.Once
				defer once.Do(func() { close(release) })
				upstream := &stagedOpenAISSEReadCloser{
					segments: [][]byte{[]byte(relayReadyFrame), []byte(recoveryFailedFrame)},
					gates:    []<-chan struct{}{nil, release},
				}
				done := make(chan error, 1)
				svc := newKeepaliveForwardService(0)
				if !passthrough {
					// Grok drains its asynchronous scanner queue independently
					// of event boundaries. Its existing non-staged keepalive
					// flushes any complete buffered event if the initial queue
					// drain did not already do so.
					svc.cfg.Gateway.StreamKeepaliveInterval = 1
				}
				go func() {
					_, err := runRecoveryHTTPStream(svc, c, &Account{ID: 1, Platform: PlatformGrok}, upstream, passthrough)
					done <- err
				}()
				deadline := time.NewTimer(4 * time.Second)
				defer deadline.Stop()
			waitRelay:
				for {
					select {
					case <-rec.flushEvents:
						body, _ := rec.snapshot()
						if strings.Contains(body, `"type":"response.relay_ready"`) {
							break waitRelay
						}
					case <-deadline.C:
						t.Fatal("timed out waiting for the relay-ready event to be delivered")
					}
				}
				once.Do(func() { close(release) })
				select {
				case err := <-done:
					require.Error(t, err)
					var failover *UpstreamFailoverError
					require.False(t, errors.As(err, &failover), "non-OpenAI relay-ready remains actual output")
				case <-time.After(5 * time.Second):
					t.Fatal("stream did not terminate")
				}
			})
			t.Run("success_order_and_ttft", func(t *testing.T) {
				svc := newKeepaliveForwardService(0)
				for _, mode := range []string{OpenAITTFTModeSemantic, OpenAITTFTModeVisible} {
					gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{openAITTFTMode: mode})
					c, rec := newKeepaliveForwardContext()
					pr, pw := io.Pipe()
					done := make(chan struct{})
					go func() {
						defer close(done)
						defer func() { _ = pw.Close() }()
						_, _ = io.WriteString(pw, relayReadyFrame+recoveryCreatedFrame)
						time.Sleep(120 * time.Millisecond)
						_, _ = io.WriteString(pw, recoveryTextFrame+recoveryCompletedFrame)
					}()
					ttft, err := runRecoveryHTTPStream(svc, c, &Account{ID: 1, Platform: PlatformOpenAI}, pr, passthrough)
					_ = pr.Close()
					<-done
					require.NoError(t, err)
					require.NotNil(t, ttft)
					require.GreaterOrEqual(t, *ttft, 100)
					out := rec.Body.String()
					require.Equal(t, 1, strings.Count(out, `"type":"response.relay_ready"`))
					relayIdx := strings.Index(out, `"type":"response.relay_ready"`)
					createdIdx := strings.Index(out, `"type":"response.created"`)
					textIdx := strings.Index(out, `"type":"response.output_text.delta"`)
					require.GreaterOrEqual(t, relayIdx, 0)
					require.Less(t, relayIdx, createdIdx)
					require.Less(t, createdIdx, textIdx)
					require.Contains(t, out, "response.completed")
				}
			})
		})
	}
	t.Run("passthrough_bare_error_terminates_after_stopping_keepalive", func(t *testing.T) {
		rec := newOpenAIResponseFlushRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		release := make(chan struct{})
		var once sync.Once
		defer once.Do(func() { close(release) })
		rec.blockFlush = 1
		rec.flushBlocked = make(chan struct{})
		rec.releaseFlush = release
		svc := newKeepaliveForwardService(0)
		// Keep the heartbeat live without depending on the timing of a tick.
		svc.cfg.Gateway.StreamKeepaliveInterval = 3600
		stream := relayReadyFrame + recoveryCreatedFrame + "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"code\":\"invalid_encrypted_content\",\"message\":\"invalid encrypted content\"}}\n\n"
		done := make(chan error, 1)
		go func() {
			_, err := runRecoveryHTTPStream(svc, c, &Account{ID: 1, Platform: PlatformOpenAI}, io.NopCloser(strings.NewReader(stream)), true)
			done <- err
		}()
		waitOpenAIResponseFlushSignal(t, rec.flushBlocked)
		value, ok := c.Get(openAICompactSSEKeepaliveKey)
		require.True(t, ok)
		keepalive := value.(*openAICompactSSEKeepalive)
		select {
		case <-keepalive.stop:
		default:
			t.Fatal("heartbeat still running during synthesized terminal flush")
		}
		out, _ := rec.snapshot()
		require.Contains(t, out, `"type":"response.relay_ready"`)
		require.Equal(t, 1, strings.Count(out, `"type":"response.failed"`))
		require.NotContains(t, out, `"type":"error"`)
		require.Contains(t, out, "invalid_encrypted_content")
		once.Do(func() { close(release) })
		select {
		case err := <-done:
			require.Error(t, err)
			var failover *UpstreamFailoverError
			require.False(t, errors.As(err, &failover))
		case <-time.After(5 * time.Second):
			t.Fatal("stream did not terminate")
		}
	})
}

func TestOpenAINativeStreamRelayReadyDoesNotDisarmFirstOutputTimeout(t *testing.T) {
	c, rec := newKeepaliveForwardContext()
	svc := newKeepaliveForwardService(1)
	start := time.Now()
	_, err := svc.handleStreamingResponse(c.Request.Context(), stalledKeepaliveResponse(relayReadyFrame), c,
		&Account{ID: 1, Platform: PlatformOpenAI}, start, "model", "model")
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover)
	require.True(t, failover.SafeToFailoverAfterWrite)
	require.Less(t, time.Since(start), 3*time.Second)
	require.Empty(t, rec.Body.String())
}

func TestOpenAIHTTPStreamPreOutputRecoveryBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, passthrough := range []bool{false, true} {
		name := "native"
		if passthrough {
			name = "passthrough"
		}
		t.Run(name, func(t *testing.T) {
			for _, tc := range []struct {
				name     string
				body     func() io.ReadCloser
				failover bool
			}{
				{"failed", func() io.ReadCloser {
					return io.NopCloser(strings.NewReader(recoveryCreatedFrame + recoveryFailedFrame))
				}, true},
				{"bare_error", func() io.ReadCloser {
					return io.NopCloser(strings.NewReader(recoveryCreatedFrame + recoveryBareErrorFrame))
				}, true},
				{"read_error", func() io.ReadCloser { return errReadCloser{err: io.ErrUnexpectedEOF} }, true},
				{"missing_terminal", func() io.ReadCloser { return io.NopCloser(strings.NewReader(recoveryCreatedFrame)) }, true},
				{"canceled", func() io.ReadCloser { return errReadCloser{err: context.Canceled} }, false},
				{"after_content", func() io.ReadCloser {
					return io.NopCloser(strings.NewReader(recoveryCreatedFrame + recoveryTextFrame + recoveryFailedFrame))
				}, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					c, rec := newKeepaliveForwardContext()
					writeRecoveryQueueHeartbeat(t, c)
					_, err := runRecoveryHTTPStream(newKeepaliveForwardService(0), c, &Account{ID: 1, Platform: PlatformOpenAI}, tc.body(), passthrough)
					require.Error(t, err)
					var failover *UpstreamFailoverError
					require.Equal(t, tc.failover, errors.As(err, &failover))
					if tc.failover || tc.name == "canceled" {
						require.Equal(t, queueHeartbeatFrame, rec.Body.String())
					} else {
						require.Contains(t, rec.Body.String(), "hello")
					}
				})
			}
			for _, tc := range []struct {
				name         string
				relay, queue bool
			}{
				{"none", false, false}, {"relay", true, false}, {"queue", false, true}, {"both", true, true},
			} {
				t.Run("nonrecoverable/"+tc.name, func(t *testing.T) {
					c, rec := newKeepaliveForwardContext()
					// Use the untagged test fixture for the existing passthrough rule so
					// this file also compiles without the unit build tag.
					rule := newNonFailoverPassthroughRule(http.StatusBadRequest, "input exceeds the context window", http.StatusBadRequest, "")
					rule.Platforms = []string{PlatformOpenAI}
					rule.PassthroughBody = true
					rule.CustomMessage = nil
					rules := &ErrorPassthroughService{}
					rules.setLocalCache([]*model.ErrorPassthroughRule{rule})
					BindErrorPassthroughService(c, rules)
					if tc.queue {
						writeRecoveryQueueHeartbeat(t, c)
					}
					stream := recoveryCreatedFrame + "event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"error\":{\"type\":\"invalid_request_error\",\"code\":\"invalid_request_error\",\"message\":\"Your input exceeds the context window of this model.\"}}}\n\n"
					if tc.relay {
						stream = relayReadyFrame + stream
					}
					_, err := runRecoveryHTTPStream(newKeepaliveForwardService(0), c, &Account{ID: 1, Platform: PlatformOpenAI}, io.NopCloser(strings.NewReader(stream)), passthrough)
					require.Error(t, err)
					var failover *UpstreamFailoverError
					require.False(t, errors.As(err, &failover))
					val, ok := c.Get(OpsUpstreamErrorsKey)
					require.True(t, ok)
					events := val.([]*OpsUpstreamErrorEvent)
					require.NotEmpty(t, events)
					if tc.relay || tc.queue {
						require.Equal(t, http.StatusOK, rec.Code)
						require.Contains(t, rec.Header().Get("Content-Type"), "text/event-stream")
						require.Contains(t, rec.Body.String(), "response.failed")
						require.Contains(t, rec.Body.String(), `"code":"context_length_exceeded"`)
						require.Equal(t, "stream_failed", events[len(events)-1].Kind)
						for _, line := range strings.Split(rec.Body.String(), "\n") {
							require.False(t, strings.HasPrefix(line, "{"), "bare JSON in SSE: %q", line)
							if data, ok := extractOpenAISSEDataLine(line); ok {
								require.True(t, gjson.Valid(data), "invalid SSE data: %q", data)
							}
						}
					} else {
						require.Equal(t, http.StatusBadRequest, rec.Code)
						require.True(t, gjson.Valid(rec.Body.String()))
						require.Equal(t, "http_error", events[len(events)-1].Kind)
					}
				})
				t.Run("compact/"+tc.name, func(t *testing.T) {
					c, _ := newKeepaliveForwardContext()
					MarkOpenAINativeCompactionV2(c)
					if tc.queue {
						writeRecoveryQueueHeartbeat(t, c)
					}
					stream := "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"type\":\"invalid_request_error\",\"code\":\"context_length_exceeded\",\"message\":\"context window exceeded\"}}}\n\n"
					if tc.relay {
						stream = relayReadyFrame + stream
					}
					_, err := runRecoveryHTTPStream(newKeepaliveForwardService(0), c, &Account{ID: 1, Platform: PlatformOpenAI}, io.NopCloser(strings.NewReader(stream)), passthrough)
					require.Error(t, err)
					_, compact := asOpenAICompactFallbackSignal(err)
					require.Equal(t, !tc.queue && !tc.relay, compact)
					var failover *UpstreamFailoverError
					require.False(t, errors.As(err, &failover))
				})
			}
		})
	}
	t.Run("native_idle_budget", func(t *testing.T) {
		c, rec := newKeepaliveForwardContext()
		writeRecoveryQueueHeartbeat(t, c)
		svc := newKeepaliveForwardService(0)
		svc.cfg.Gateway.StreamDataIntervalTimeout = 1
		_, err := svc.handleStreamingResponse(c.Request.Context(), stalledKeepaliveResponse(), c,
			&Account{ID: 1, Platform: PlatformOpenAI}, time.Now(), "model", "model")
		var failover *UpstreamFailoverError
		require.ErrorAs(t, err, &failover)
		require.True(t, failover.SafeToFailoverAfterWrite)
		require.Equal(t, queueHeartbeatFrame, rec.Body.String())
	})
}

// keepaliveForwardBody holds the upstream stream open until the gateway closes it.
type keepaliveForwardBody struct {
	io.ReadCloser
	closed chan struct{}
	once   sync.Once
}

func (b *keepaliveForwardBody) Close() error {
	b.once.Do(func() { close(b.closed) })
	return b.ReadCloser.Close()
}

// stalledKeepaliveResponse returns an upstream response that sends the preamble and the given
// keepalive frames, then produces no semantic output until it is closed.
func stalledKeepaliveResponse(frames ...string) *http.Response {
	pr, pw := io.Pipe()
	body := &keepaliveForwardBody{ReadCloser: pr, closed: make(chan struct{})}
	go func() {
		defer func() { _ = pw.Close() }()
		_, _ = pw.Write([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_stalled\"}}\n\n"))
		_, _ = pw.Write([]byte("data: {\"type\":\"response.in_progress\",\"response\":{\"id\":\"resp_stalled\"}}\n\n"))
		for _, frame := range frames {
			_, _ = pw.Write([]byte(frame))
		}
		select {
		case <-body.closed:
		case <-time.After(5 * time.Second):
		}
	}()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":                   []string{"text/event-stream"},
			"X-Request-Id":                   []string{"request-stalled"},
			"X-Ratelimit-Remaining-Requests": []string{"1"},
		},
		Body: body,
	}
}

func newKeepaliveForwardService(firstOutputTimeoutSeconds int) *OpenAIGatewayService {
	cfg := &config.Config{Gateway: config.GatewayConfig{
		OpenAIFirstOutputTimeoutSeconds: firstOutputTimeoutSeconds,
		MaxLineSize:                     defaultMaxLineSize,
	}}
	return &OpenAIGatewayService{cfg: cfg, responseHeaderFilter: compileResponseHeaderFilter(cfg)}
}

func newKeepaliveForwardContext() (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	return c, rec
}

const (
	upstreamKeepaliveData  = `data: {"type":"keepalive","sequence_number":3}`
	upstreamKeepaliveEvent = "event: keepalive\n" + upstreamKeepaliveData + "\n\n"
)

// Codex resets its stream idle timer only on parsed SSE events, so the upstream keepalive event
// must reach the client before the first semantic output, while the attempt's preamble and
// account headers stay private and the bytes still do not count as client output.
func TestOpenAINativeStreamForwardsUpstreamKeepaliveBeforeFirstOutput(t *testing.T) {
	for _, tc := range []struct {
		name  string
		frame string
	}{
		{"event_and_data", upstreamKeepaliveEvent},
		{"data_only", upstreamKeepaliveData + "\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newKeepaliveForwardService(1)
			c, rec := newKeepaliveForwardContext()

			_, err := svc.handleStreamingResponse(c.Request.Context(), stalledKeepaliveResponse(tc.frame), c,
				&Account{ID: 1, Platform: PlatformOpenAI}, time.Now(), "model", "model")

			var failoverErr *UpstreamFailoverError
			require.ErrorAs(t, err, &failoverErr, "no semantic output: the first-output timeout must fail over")
			require.Equal(t, tc.frame, rec.Body.String(), "only the upstream keepalive event reaches the client, verbatim")
			require.Equal(t, -1, OpenAICompactKeepaliveAdjustedWrittenSize(c), "keepalive bytes are not client output")
			require.Empty(t, rec.Result().Header.Values("X-Request-Id"), "account headers stay private before output")
			require.Empty(t, rec.Result().Header.Values("X-Ratelimit-Remaining-Requests"))
		})
	}
}

// After a pre-output failover the next account's stream continues the same response: the client
// sees the first account's keepalive, then the second account's complete stream from its start.
func TestOpenAINativeStreamKeepaliveThenFailoverContinuesWithNextAccount(t *testing.T) {
	svc := newKeepaliveForwardService(1)
	c, rec := newKeepaliveForwardContext()

	_, firstErr := svc.handleStreamingResponse(c.Request.Context(), stalledKeepaliveResponse(upstreamKeepaliveEvent), c,
		&Account{ID: 1, Platform: PlatformOpenAI}, time.Now(), "model", "model")
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, firstErr, &failoverErr)

	secondResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"request-second"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.created","response":{"id":"resp_second"}}`,
			"",
			`data: {"type":"response.output_text.delta","delta":"hello"}`,
			"",
			`data: {"type":"response.completed","response":{"id":"resp_second","usage":{"input_tokens":1,"output_tokens":1}}}`,
			"",
		}, "\n"))),
	}
	result, secondErr := svc.handleStreamingResponse(c.Request.Context(), secondResp, c,
		&Account{ID: 2, Platform: PlatformOpenAI}, time.Now(), "model", "model")
	require.NoError(t, secondErr)
	require.NotNil(t, result)

	body := rec.Body.String()
	require.True(t, strings.HasPrefix(body, upstreamKeepaliveEvent), "first account's keepalive comes first, got %q", body)
	require.NotContains(t, body, "resp_stalled", "the failed attempt's preamble never reaches the client")
	created := strings.Index(body, `"id":"resp_second"`)
	require.Greater(t, created, len(upstreamKeepaliveEvent)-1)
	require.Less(t, created, strings.Index(body, `"delta":"hello"`))
	require.Empty(t, rec.Result().Header.Values("X-Request-Id"), "headers were committed by the keepalive, not the second account")
}

// Keepalive events after output started keep their place in the stream: not dropped, not repeated.
func TestOpenAINativeStreamKeepaliveAfterOutputStaysInOrder(t *testing.T) {
	svc := newKeepaliveForwardService(0)
	c, rec := newKeepaliveForwardContext()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"type":"response.created","response":{"id":"resp_order"}}`,
			"",
			`data: {"type":"response.output_text.delta","delta":"a"}`,
			"",
			"event: keepalive",
			upstreamKeepaliveData,
			"",
			`data: {"type":"response.output_text.delta","delta":"b"}`,
			"",
			`data: {"type":"response.completed","response":{"id":"resp_order","usage":{"input_tokens":1,"output_tokens":2}}}`,
			"",
		}, "\n"))),
	}

	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI}, time.Now(), "model", "model")
	require.NoError(t, err)

	body := rec.Body.String()
	require.Equal(t, 1, strings.Count(body, `"type":"keepalive"`))
	a, ka, b := strings.Index(body, `"delta":"a"`), strings.Index(body, upstreamKeepaliveEvent), strings.Index(body, `"delta":"b"`)
	require.True(t, a < ka && ka < b, "keepalive must stay between the deltas, got %q", body)
}

// Forwarded keepalive events are not output: they must not postpone the first-output timeout,
// so an attempt whose upstream only keeps the stream alive still fails over on time.
func TestOpenAINativeStreamKeepaliveDoesNotResetFirstOutputTimeout(t *testing.T) {
	pr, pw := io.Pipe()
	body := &keepaliveForwardBody{ReadCloser: pr, closed: make(chan struct{})}
	go func() {
		defer func() { _ = pw.Close() }()
		_, _ = pw.Write([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_paced\"}}\n\n"))
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		deadline := time.After(4 * time.Second)
		for {
			select {
			case <-body.closed:
				return
			case <-deadline:
				return
			case <-ticker.C:
				if _, err := pw.Write([]byte(upstreamKeepaliveEvent)); err != nil {
					return
				}
			}
		}
	}()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       body,
	}
	svc := newKeepaliveForwardService(1)
	c, rec := newKeepaliveForwardContext()

	started := time.Now()
	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI}, started, "model", "model")
	elapsed := time.Since(started)

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Less(t, elapsed, 2500*time.Millisecond, "keepalive events must not reset the first-output timer")
	require.GreaterOrEqual(t, strings.Count(rec.Body.String(), upstreamKeepaliveEvent), 2, "keepalives reached the client before the timeout")
	require.NotContains(t, rec.Body.String(), "resp_paced")
	require.Equal(t, -1, OpenAICompactKeepaliveAdjustedWrittenSize(c))
}
