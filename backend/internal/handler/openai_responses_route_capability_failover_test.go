//go:build unit

package handler

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type routeCapabilityTestResponse struct {
	status int
	body   string
}

type routeCapabilityTestUpstream struct {
	service.HTTPUpstream
	t             *testing.T
	responses     map[int64][]routeCapabilityTestResponse
	calls         []int64
	requestBodies [][]byte
}

func (u *routeCapabilityTestUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.t.Helper()
	body, err := io.ReadAll(req.Body)
	require.NoError(u.t, err)
	u.calls = append(u.calls, accountID)
	u.requestBodies = append(u.requestBodies, body)
	responses := u.responses[accountID]
	require.NotEmpty(u.t, responses, "账号 %d 收到未预期的上游调用", accountID)
	response := responses[0]
	u.responses[accountID] = responses[1:]
	return &http.Response{
		StatusCode: response.status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(response.body)),
	}, nil
}

func newRouteCapabilityTestHandler(t *testing.T, upstream service.HTTPUpstream, maxSwitches int, retry400 bool) *OpenAIGatewayHandler {
	t.Helper()
	accounts := make([]service.Account, 0, 6)
	for id := 1; id <= 6; id++ {
		accounts = append(accounts, service.Account{
			ID:          int64(id),
			Name:        fmt.Sprintf("responses-account-%d", id),
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeAPIKey,
			Status:      service.StatusActive,
			Schedulable: true,
			Concurrency: 0,
			Priority:    id,
			Credentials: map[string]any{
				"api_key":   "test-key",
				"base_url":  "https://api.example.test",
				"pool_mode": true,
			},
		})
	}
	if retry400 {
		accounts[0].Credentials["pool_mode_retry_status_codes"] = []any{float64(http.StatusBadRequest)}
		accounts[0].Credentials["pool_mode_retry_count"] = float64(1)
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.MaxAccountSwitches = maxSwitches
	gateway := service.NewOpenAIGatewayService(
		openAIImagesFailoverAccountRepo{accounts: accounts},
		nil, nil, nil, nil, nil, nil,
		cfg,
		nil, nil, service.NewBillingService(cfg, nil), nil, nil,
		upstream,
		&service.DeferredService{},
		nil, nil, nil, nil, nil, nil, nil,
	)
	billingCache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCache.Stop)
	return NewOpenAIGatewayHandler(
		gateway,
		service.NewConcurrencyService(nil),
		billingCache,
		service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg),
		nil, nil, nil, nil, cfg,
	)
}

func TestOpenAIResponsesRouteCapability(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const capabilityErrorCode = "unsupported_parameter"
	const invalidRequestType = "invalid_request_error"
	const capabilityMessage = "The available upstream route does not support a requested input or tool capability."
	const ordinaryMessage = "Unsupported parameter: 'temperature'"
	badRequest := func(errorType, message string) routeCapabilityTestResponse {
		return routeCapabilityTestResponse{
			status: http.StatusBadRequest,
			body:   fmt.Sprintf(`{"error":{"code":%q,"message":%q,"param":"","type":%q}}`, capabilityErrorCode, message, errorType),
		}
	}
	accountMessage := func(id int64) string {
		return fmt.Sprintf("%s (request id: test-%d)", capabilityMessage, id)
	}
	capabilityError := badRequest(invalidRequestType, capabilityMessage)
	busy := routeCapabilityTestResponse{
		status: http.StatusBadGateway,
		body:   `{"error":{"message":"upstream busy"}}`,
	}
	success := routeCapabilityTestResponse{
		status: http.StatusOK,
		body:   `{"id":"resp_ok","object":"response","model":"gpt-5.1","status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`,
	}

	t.Run("H1_success_after_switch", func(t *testing.T) {
		upstream := &routeCapabilityTestUpstream{t: t, responses: map[int64][]routeCapabilityTestResponse{
			1: {capabilityError}, 2: {success},
		}}
		handler := newRouteCapabilityTestHandler(t, upstream, 10, false)
		c, rec := newOpenAIResponsesFailoverTestContext(t, nil)
		handler.Responses(c)

		require.Equal(t, []int64{1, 2}, upstream.calls)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, "resp_ok", gjson.GetBytes(rec.Body.Bytes(), "id").String())
	})

	t.Run("H2_last_error_after_three_switches", func(t *testing.T) {
		responses := make(map[int64][]routeCapabilityTestResponse)
		for id := int64(1); id <= 6; id++ {
			responses[id] = []routeCapabilityTestResponse{badRequest(invalidRequestType, accountMessage(id))}
		}
		responses[4][0] = badRequest("custom_error", accountMessage(4))
		upstream := &routeCapabilityTestUpstream{t: t, responses: responses}
		handler := newRouteCapabilityTestHandler(t, upstream, 10, false)
		c, rec := newOpenAIResponsesFailoverTestContext(t, nil)
		handler.Responses(c)

		require.Equal(t, []int64{1, 2, 3, 4}, upstream.calls)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Equal(t, capabilityErrorCode, gjson.GetBytes(rec.Body.Bytes(), "error.code").String())
		require.Equal(t, "custom_error", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
		require.Equal(t, accountMessage(4), gjson.GetBytes(rec.Body.Bytes(), "error.message").String())
	})

	t.Run("H3_switch_budget", func(t *testing.T) {
		for _, tc := range []struct {
			name        string
			maxSwitches int
			retry400    bool
			wantCalls   []int64
			wantStatus  int
		}{
			{"three_more_after_first_switch", 10, false, []int64{1, 2, 3, 4, 5}, http.StatusBadRequest},
			{"original_limit_is_lower", 2, false, []int64{1, 2, 3}, http.StatusBadRequest},
			{"budget_is_reduced_before_same_account_retry", 10, true, []int64{1, 1, 2, 3, 4}, http.StatusBadGateway},
		} {
			t.Run(tc.name, func(t *testing.T) {
				responses := map[int64][]routeCapabilityTestResponse{1: {busy}}
				for id := int64(2); id <= 6; id++ {
					if tc.retry400 {
						responses[id] = []routeCapabilityTestResponse{busy}
					} else {
						responses[id] = []routeCapabilityTestResponse{capabilityError}
					}
				}
				if tc.retry400 {
					responses[1] = []routeCapabilityTestResponse{capabilityError, busy}
				}
				upstream := &routeCapabilityTestUpstream{t: t, responses: responses}
				handler := newRouteCapabilityTestHandler(t, upstream, tc.maxSwitches, tc.retry400)
				c, rec := newOpenAIResponsesFailoverTestContext(t, nil)
				handler.Responses(c)

				require.Equal(t, tc.wantCalls, upstream.calls)
				require.Equal(t, tc.wantStatus, rec.Code)
			})
		}
	})

	t.Run("H4_ordinary_400_does_not_switch", func(t *testing.T) {
		upstream := &routeCapabilityTestUpstream{t: t, responses: map[int64][]routeCapabilityTestResponse{
			1: {badRequest(invalidRequestType, ordinaryMessage)},
		}}
		handler := newRouteCapabilityTestHandler(t, upstream, 10, false)
		c, rec := newOpenAIResponsesFailoverTestContext(t, nil)
		handler.Responses(c)

		require.Equal(t, []int64{1}, upstream.calls)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Equal(t, ordinaryMessage, gjson.GetBytes(rec.Body.Bytes(), "error.message").String())
	})

	t.Run("H5_continuation_does_not_switch", func(t *testing.T) {
		upstream := &routeCapabilityTestUpstream{t: t, responses: map[int64][]routeCapabilityTestResponse{
			1: {capabilityError}, 2: {success},
		}}
		handler := newRouteCapabilityTestHandler(t, upstream, 10, false)
		c, rec := newOpenAIResponsesFailoverTestContext(t, nil)
		c.Request.Body = io.NopCloser(strings.NewReader(`{"model":"gpt-5.1","stream":false,"messages":[{"role":"user","content":"hello"}],"previous_response_id":"resp_prev"}`))
		require.NoError(t, handler.gatewayService.BindOpenAIHTTPResponseOwner(c.Request.Context(), 3131, "resp_prev", 100, 99))
		handler.Responses(c)

		require.Equal(t, []int64{1}, upstream.calls)
		require.False(t, gjson.GetBytes(upstream.requestBodies[0], "previous_response_id").Exists())
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Equal(t, capabilityErrorCode, gjson.GetBytes(rec.Body.Bytes(), "error.code").String())
		require.Equal(t, invalidRequestType, gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
		require.Equal(t, capabilityMessage, gjson.GetBytes(rec.Body.Bytes(), "error.message").String())
	})
}
