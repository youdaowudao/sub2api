package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestConcurrencyErrorResponse(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		slotType    string
		wantStatus  int
		wantType    string
		wantCode    string
		wantMessage string
	}{
		{
			name:        "true concurrency timeout remains rate limit",
			err:         &ConcurrencyError{SlotType: "account", IsTimeout: true},
			slotType:    "user",
			wantStatus:  http.StatusTooManyRequests,
			wantType:    "rate_limit_error",
			wantCode:    gatewayConcurrencyLimitCode,
			wantMessage: "Concurrency limit exceeded for account, please retry later",
		},
		{
			name:        "full local wait queue has gateway code",
			err:         &WaitQueueFullError{SlotType: "account"},
			slotType:    "account",
			wantStatus:  http.StatusTooManyRequests,
			wantType:    "rate_limit_error",
			wantCode:    gatewayQueueFullCode,
			wantMessage: "Too many pending requests, please retry later（排队请求过多，请稍后重试）",
		},
		{
			name:        "user wait queue has Chinese hint",
			err:         &WaitQueueFullError{SlotType: "user"},
			slotType:    "user",
			wantStatus:  http.StatusTooManyRequests,
			wantType:    "rate_limit_error",
			wantCode:    gatewayQueueFullCode,
			wantMessage: "Too many pending requests, please retry later（排队请求过多，请稍后重试）",
		},
		{
			name:        "error user slot overrides account argument",
			err:         &ConcurrencyError{SlotType: "user", IsTimeout: true},
			slotType:    "account",
			wantStatus:  http.StatusTooManyRequests,
			wantType:    "rate_limit_error",
			wantCode:    gatewayConcurrencyLimitCode,
			wantMessage: "Concurrency limit exceeded for user, please retry later（并发数已达上限，请稍后重试）",
		},
		{
			name:        "empty error slot uses user argument",
			err:         &ConcurrencyError{IsTimeout: true},
			slotType:    "user",
			wantStatus:  http.StatusTooManyRequests,
			wantType:    "rate_limit_error",
			wantCode:    gatewayConcurrencyLimitCode,
			wantMessage: "Concurrency limit exceeded for user, please retry later（并发数已达上限，请稍后重试）",
		},
		{
			name:        "empty error slot keeps account argument English",
			err:         &ConcurrencyError{IsTimeout: true},
			slotType:    "account",
			wantStatus:  http.StatusTooManyRequests,
			wantType:    "rate_limit_error",
			wantCode:    gatewayConcurrencyLimitCode,
			wantMessage: "Concurrency limit exceeded for account, please retry later",
		},
		{
			name:        "other final slot stays English",
			err:         &ConcurrencyError{SlotType: "other", IsTimeout: true},
			slotType:    "user",
			wantStatus:  http.StatusTooManyRequests,
			wantType:    "rate_limit_error",
			wantCode:    gatewayConcurrencyLimitCode,
			wantMessage: "Concurrency limit exceeded for other, please retry later",
		},
		{
			name:        "client cancellation is not classified as concurrency limit",
			err:         context.Canceled,
			slotType:    "user",
			wantStatus:  statusClientClosedRequest,
			wantType:    "api_error",
			wantMessage: "context canceled",
		},
		{
			name:        "deadline exceeded is service unavailable",
			err:         context.DeadlineExceeded,
			slotType:    "user",
			wantStatus:  http.StatusServiceUnavailable,
			wantType:    "api_error",
			wantMessage: "Service temporarily unavailable, please retry later（服务暂时不可用，请稍后重试）",
		},
		{
			name:        "redis acquire error is service unavailable",
			err:         errors.New("redis unavailable"),
			slotType:    "user",
			wantStatus:  http.StatusServiceUnavailable,
			wantType:    "api_error",
			wantMessage: "Service temporarily unavailable, please retry later（服务暂时不可用，请稍后重试）",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, errType, code, message := concurrencyErrorResponse(tt.err, tt.slotType)
			require.Equal(t, tt.wantStatus, status)
			require.Equal(t, tt.wantType, errType)
			require.Equal(t, tt.wantCode, code)
			require.Equal(t, tt.wantMessage, message)
		})
	}
}

func TestGoogleConcurrencyError(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantStatus  int
		wantGStatus string
		wantMessage string
	}{
		{
			name:        "client cancellation is 499",
			err:         context.Canceled,
			wantStatus:  statusClientClosedRequest,
			wantGStatus: "CANCELLED",
			wantMessage: "context canceled",
		},
		{
			name:        "full wait queue is 429",
			err:         &WaitQueueFullError{SlotType: "user"},
			wantStatus:  http.StatusTooManyRequests,
			wantGStatus: "RESOURCE_EXHAUSTED",
			wantMessage: "Too many pending requests, please retry later（排队请求过多，请稍后重试）",
		},
		{
			name:        "slot wait timeout is 429",
			err:         &ConcurrencyError{SlotType: "user", IsTimeout: true},
			wantStatus:  http.StatusTooManyRequests,
			wantGStatus: "RESOURCE_EXHAUSTED",
			wantMessage: "Concurrency limit exceeded for user, please retry later（并发数已达上限，请稍后重试）",
		},
		{
			name:        "acquire backend error is 503",
			err:         errors.New("redis unavailable"),
			wantStatus:  http.StatusServiceUnavailable,
			wantGStatus: "INTERNAL",
			wantMessage: "Service temporarily unavailable, please retry later（服务暂时不可用，请稍后重试）",
		},
	}

	gin.SetMode(gin.TestMode)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)

			googleConcurrencyError(c, tt.err, "user")

			require.Equal(t, tt.wantStatus, rec.Code)
			var body struct {
				Error struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
					Status  string `json:"status"`
				} `json:"error"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			require.Equal(t, tt.wantStatus, body.Error.Code)
			require.Equal(t, tt.wantGStatus, body.Error.Status)
			require.Equal(t, tt.wantMessage, body.Error.Message)
		})
	}
}
