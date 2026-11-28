package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type metricListStub struct {
	*stubAdminService
	calls int
}

func (s *metricListStub) ListAccountsByMetric(_ context.Context, params pagination.PaginationParams, _, _, _, _ string, _ int64, _ string, _ int64, _, _ time.Time) (service.AccountMetricSortResult, error) {
	s.calls++
	out := service.AccountMetricSortResult{Accounts: []service.Account{{ID: 5, Name: "metric", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive}}, Total: 1}
	if params.SortBy == service.AccountSortCacheHitRate {
		rate := 82.5
		out.CacheStats = map[int64]usagestats.CacheHitRateItem{5: {ID: 5, Requests: 3, CacheHitRate: &rate}}
	} else {
		out.TodayStats = map[int64]*usagestats.AccountStats{5: {Requests: 9, Tokens: 11, Cost: 2, UserCost: 3}}
	}
	return out, nil
}

func TestAccountMetricListResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &metricListStub{stubAdminService: newStubAdminService()}
	h := &AccountHandler{adminService: svc}
	router := gin.New()
	router.GET("/accounts", h.List)
	router.GET("/rates", h.GetUpstreamBillingRates)
	for _, q := range []string{
		"sort_by=cache_hit_rate&start_date=" + time.Now().UTC().Format("2006-01-02") + "&end_date=" + time.Now().UTC().Format("2006-01-02") + "&timezone=UTC",
		"sort_by=today_requests",
	} {
		for _, lite := range []string{"0", "1"} {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/accounts?"+q+"&lite="+lite, nil)
			router.ServeHTTP(rec, req)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			var payload struct {
				Data struct {
					Items []map[string]json.RawMessage `json:"items"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))
			require.Len(t, payload.Data.Items, 1)
			if q == "sort_by=today_requests" {
				require.Contains(t, payload.Data.Items[0], "today_stats")
			} else {
				require.Contains(t, payload.Data.Items[0], "cache_hit_rate_stats")
			}
			tag := rec.Header().Get("ETag")
			require.NotEmpty(t, tag)
			req = httptest.NewRequest(http.MethodGet, "/accounts?"+q+"&lite="+lite, nil)
			req.Header.Set("If-None-Match", tag)
			rec = httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			require.Equal(t, http.StatusNotModified, rec.Code)
		}
	}
	require.Equal(t, 8, svc.calls)
	for _, query := range []string{"account_ids=3,7,2", "account_ids=3,3", "account_ids=3&sort_by=name", "account_ids=0"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/rates?"+query, nil))
		if query == "account_ids=3,7,2" {
			require.Equal(t, 200, rec.Code)
			require.Contains(t, rec.Body.String(), "\"account_id\":3")
		} else {
			require.Equal(t, 400, rec.Code)
		}
	}
}
