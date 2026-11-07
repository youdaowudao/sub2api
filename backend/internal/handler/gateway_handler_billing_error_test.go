package handler

import (
	"errors"
	"net/http"
	"testing"
	"time"

	pkgerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBillingErrorDetails_ChineseHints(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		status     int
		code       string
		message    string
		retryAfter bool
	}{
		{"balance", service.ErrInsufficientBalance, http.StatusForbidden, "billing_error", "insufficient balance（账户余额不足，请及时充值）", false},
		{"billing service", service.ErrBillingServiceUnavailable, http.StatusServiceUnavailable, "billing_service_error", "Billing service temporarily unavailable. Please retry later.（计费服务暂时不可用，请稍后重试）", false},
		{"billing fallback", pkgerrors.ServiceUnavailable("BILLING_SERVICE_ERROR", ""), http.StatusServiceUnavailable, "billing_service_error", "Billing service temporarily unavailable. Please retry later.（计费服务暂时不可用，请稍后重试）", false},
		{"group RPM", service.ErrGroupRPMExceeded, http.StatusTooManyRequests, "rate_limit_exceeded", "group requests-per-minute limit exceeded（请求过于频繁，请稍后再试）", true},
		{"user RPM", service.ErrUserRPMExceeded, http.StatusTooManyRequests, "rate_limit_exceeded", "user requests-per-minute limit exceeded（请求过于频繁，请稍后再试）", true},
		{"platform daily", service.ErrUserPlatformDailyQuotaExhausted, http.StatusTooManyRequests, "rate_limit_exceeded", "Daily usage quota exhausted for this platform.（该平台日额度已用完，请等待重置）", true},
		{"platform weekly", service.ErrUserPlatformWeeklyQuotaExhausted, http.StatusTooManyRequests, "rate_limit_exceeded", "Weekly usage quota exhausted for this platform.（该平台周额度已用完，请等待重置）", true},
		{"platform monthly", service.ErrUserPlatformMonthlyQuotaExhausted, http.StatusTooManyRequests, "rate_limit_exceeded", "Monthly usage quota exhausted for this platform.（该平台月额度已用完，请等待重置）", true},
		{"subscription invalid", service.ErrSubscriptionInvalid, http.StatusForbidden, "billing_error", "subscription is invalid or expired（订阅无效或已过期）", false},
		{"subscription daily", service.ErrDailyLimitExceeded, http.StatusForbidden, "billing_error", "daily usage limit exceeded（订阅日用量已达上限，请等待重置）", false},
		{"subscription weekly", service.ErrWeeklyLimitExceeded, http.StatusForbidden, "billing_error", "weekly usage limit exceeded（订阅周用量已达上限，请等待重置）", false},
		{"subscription monthly", service.ErrMonthlyLimitExceeded, http.StatusForbidden, "billing_error", "monthly usage limit exceeded（订阅月用量已达上限，请等待重置）", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, code, message, retryAfter := billingErrorDetails(tt.err)
			require.Equal(t, tt.status, status)
			require.Equal(t, tt.code, code)
			require.Equal(t, tt.message, message)
			if tt.retryAfter {
				require.Greater(t, retryAfter, 0)
				require.LessOrEqual(t, retryAfter, 60)
			} else {
				require.Zero(t, retryAfter)
			}
		})
	}
}

func TestMapUpstreamError_PreservesEnglish(t *testing.T) {
	tests := []struct {
		name           string
		upstreamStatus int
		status         int
		gatewayType    string
		openaiType     string
		message        string
	}{
		{"401", 401, http.StatusBadGateway, "upstream_error", "upstream_error", "Upstream authentication failed, please contact administrator"},
		{"403", 403, http.StatusBadGateway, "upstream_error", "upstream_error", "Upstream access forbidden, please contact administrator"},
		{"429", 429, http.StatusTooManyRequests, "rate_limit_error", "rate_limit_error", "Upstream rate limit exceeded, please retry later"},
		{"529", 529, http.StatusServiceUnavailable, "overloaded_error", "upstream_error", "Upstream service overloaded, please retry later"},
		{"500", 500, http.StatusBadGateway, "upstream_error", "upstream_error", "Upstream service temporarily unavailable"},
		{"502", 502, http.StatusBadGateway, "upstream_error", "upstream_error", "Upstream service temporarily unavailable"},
		{"503", 503, http.StatusBadGateway, "upstream_error", "upstream_error", "Upstream service temporarily unavailable"},
		{"504", 504, http.StatusBadGateway, "upstream_error", "upstream_error", "Upstream service temporarily unavailable"},
		{"default", 418, http.StatusBadGateway, "upstream_error", "upstream_error", "Upstream request failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, errType, message := (&GatewayHandler{}).mapUpstreamError(tt.upstreamStatus)
			require.Equal(t, tt.status, status)
			require.Equal(t, tt.gatewayType, errType)
			require.Equal(t, tt.message, message)
			require.NotContains(t, message, "（")

			status, errType, message = (&OpenAIGatewayHandler{}).mapUpstreamError(tt.upstreamStatus)
			require.Equal(t, tt.status, status)
			require.Equal(t, tt.openaiType, errType)
			require.Equal(t, tt.message, message)
			require.NotContains(t, message, "（")
		})
	}
}

func TestBillingErrorDetails_MapsGroupRPMExceededToTooManyRequests(t *testing.T) {
	status, code, msg, retryAfter := billingErrorDetails(service.ErrGroupRPMExceeded)
	require.Equal(t, http.StatusTooManyRequests, status)
	require.Equal(t, "rate_limit_exceeded", code)
	require.NotEmpty(t, msg)
	require.Greater(t, retryAfter, 0, "RPM exceeded should return positive Retry-After")
	require.LessOrEqual(t, retryAfter, 60)
}

func TestBillingErrorDetails_MapsUserRPMExceededToTooManyRequests(t *testing.T) {
	status, code, msg, retryAfter := billingErrorDetails(service.ErrUserRPMExceeded)
	require.Equal(t, http.StatusTooManyRequests, status)
	require.Equal(t, "rate_limit_exceeded", code)
	require.NotEmpty(t, msg)
	require.Greater(t, retryAfter, 0, "RPM exceeded should return positive Retry-After")
	require.LessOrEqual(t, retryAfter, 60)
}

func TestBillingErrorDetails_APIKeyRateLimitStillMaps(t *testing.T) {
	// 回归保护：加 RPM 分支后不应影响已有 APIKey rate limit 的映射。
	for _, err := range []error{
		service.ErrAPIKeyRateLimit5hExceeded,
		service.ErrAPIKeyRateLimit1dExceeded,
		service.ErrAPIKeyRateLimit7dExceeded,
	} {
		status, code, _, _ := billingErrorDetails(err)
		require.Equal(t, http.StatusTooManyRequests, status, "status for %v", err)
		require.Equal(t, "rate_limit_exceeded", code)
	}
}

func TestBillingErrorDetails_BillingServiceUnavailableMapsTo503(t *testing.T) {
	status, code, _, retryAfter := billingErrorDetails(service.ErrBillingServiceUnavailable)
	require.Equal(t, http.StatusServiceUnavailable, status)
	require.Equal(t, "billing_service_error", code)
	require.Equal(t, 0, retryAfter, "non-RPM errors should not set Retry-After")
}

func TestBillingErrorDetails_UnknownErrorFallsBackTo403(t *testing.T) {
	status, code, msg, _ := billingErrorDetails(service.ErrInsufficientBalance)
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, "billing_error", code)
	require.NotEmpty(t, msg)
}

func TestExtractQuotaResetSeconds_T19_HappyPath(t *testing.T) {
	err := service.ErrUserPlatformDailyQuotaExhausted.WithMetadata(map[string]string{
		"window_resets_at": time.Now().Add(10 * time.Second).UTC().Format(time.RFC3339),
	})
	got := extractQuotaResetSeconds(err)
	if got < 10 || got > 11 {
		t.Errorf("T19: got %d, want 10 or 11 (math.Ceil boundary)", got)
	}
}

func TestExtractQuotaResetSeconds_T20_NoMetadataFallback(t *testing.T) {
	if got := extractQuotaResetSeconds(errors.New("naked error")); got != 60 {
		t.Errorf("T20: got %d, want 60 fallback", got)
	}
}

func TestExtractQuotaResetSeconds_T21_BadFormatFallback(t *testing.T) {
	err := service.ErrUserPlatformDailyQuotaExhausted.WithMetadata(map[string]string{
		"window_resets_at": "not-a-time",
	})
	if got := extractQuotaResetSeconds(err); got != 60 {
		t.Errorf("T21: got %d, want 60 fallback", got)
	}
}

func TestExtractQuotaResetSeconds_T22_PastResetFallsBackToDefault(t *testing.T) {
	// 当 window_resets_at 已过去时返回 fallback (60s) 而非 1s：
	// 1 秒会导致客户端立即重试仍触发限额的退避循环；
	// 60s 让客户端按常规节奏退避，cache/DB 自愈期间不会反复打抖。
	err := service.ErrUserPlatformDailyQuotaExhausted.WithMetadata(map[string]string{
		"window_resets_at": time.Now().Add(-5 * time.Second).UTC().Format(time.RFC3339),
	})
	if got := extractQuotaResetSeconds(err); got != 60 {
		t.Errorf("T22: got %d, want 60 (fallback on past reset)", got)
	}
}

func TestBillingErrorDetails_T10_QuotaExhaustedReturns429WithRetryAfter(t *testing.T) {
	// quota 超限映射 429 + Retry-After（RFC 6585 / 与 RPM 一致），
	// 让 SDK（OpenAI 兼容客户端等）能按 Retry-After 自动退避。
	// 旧实现用 403 导致客户端不退避直接报错。
	// 三个窗口共用同一映射分支，循环覆盖避免漏测某个窗口的 status/code。
	cases := []struct {
		name string
		err  error
	}{
		{"daily", service.ErrUserPlatformDailyQuotaExhausted.WithMetadata(map[string]string{
			"window_resets_at": time.Now().Add(60 * time.Minute).UTC().Format(time.RFC3339),
		})},
		{"weekly", service.ErrUserPlatformWeeklyQuotaExhausted.WithMetadata(map[string]string{
			"window_resets_at": time.Now().Add(60 * time.Minute).UTC().Format(time.RFC3339),
		})},
		{"monthly", service.ErrUserPlatformMonthlyQuotaExhausted.WithMetadata(map[string]string{
			"window_resets_at": time.Now().Add(60 * time.Minute).UTC().Format(time.RFC3339),
		})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, code, _, retryAfter := billingErrorDetails(tc.err)
			if status != http.StatusTooManyRequests {
				t.Errorf("status = %d, want 429", status)
			}
			if code != "rate_limit_exceeded" {
				t.Errorf("code = %q, want rate_limit_exceeded", code)
			}
			if retryAfter < 3599 || retryAfter > 3601 {
				t.Errorf("retryAfter = %d, want ~3600", retryAfter)
			}
		})
	}
}
