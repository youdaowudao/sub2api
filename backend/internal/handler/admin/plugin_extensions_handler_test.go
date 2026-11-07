package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPluginExtensionRequestRejectsInvalidEnvelopeBeforeDispatch(t *testing.T) {
	for _, body := range []string{"null", "[]", "", "{} {}", `{"payload":{},"user_id":999,"role":"admin"}`, `{"payload":"` + strings.Repeat("x", pluginCallBodyLimit) + `"}`} {
		t.Run("rejected", func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/plugins/7/calls/echo", bytes.NewBufferString(body))
			ctx.Request = ctx.Request.WithContext(context.WithValue(ctx.Request.Context(), ctxkey.RequestID, "known-http-audit-id"))
			var request struct {
				Payload json.RawMessage `json:"payload"`
			}
			require.False(t, decodePluginExtensionBody(ctx, &request))
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			var envelope response.Response
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
			require.Equal(t, "PLUGIN_INVALID_REQUEST", envelope.Reason)
			require.Equal(t, "known-http-audit-id", envelope.Metadata["request_id"])
			require.Equal(t, "false", envelope.Metadata["retryable"])
			require.Equal(t, "false", envelope.Metadata["outcome_unknown"])
		})
	}
}

func TestPluginExtensionRequestAcceptsBoundedPayloadObject(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/plugins/7/calls/echo", strings.NewReader(`{"payload":{"value":"unchanged"}}`))
	var request struct {
		Payload json.RawMessage `json:"payload"`
	}
	require.True(t, decodePluginExtensionBody(ctx, &request))
	require.JSONEq(t, `{"value":"unchanged"}`, string(request.Payload))
	require.Zero(t, recorder.Body.Len())
}
