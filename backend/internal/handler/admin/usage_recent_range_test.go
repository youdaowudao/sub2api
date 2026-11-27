package admin

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type performanceCapture struct {
	cacheRateRepoCapture
	perfCalls int
	errorRows bool
}

func (r *performanceCapture) GetAccountPerformanceStats(_ context.Context, filters usagestats.UsageLogFilters, _ []int64, _ string, errors bool) ([]usagestats.AccountPerformanceItem, error) {
	r.perfCalls++
	r.filters = filters
	r.errorRows = errors
	return []usagestats.AccountPerformanceItem{}, nil
}

func TestAdminRecentUsageWindowAndPerformance(t *testing.T) {
	repo := &performanceCapture{}
	usage := service.NewUsageService(repo, nil, nil, nil)
	h := NewUsageHandler(usage, nil, nil, nil, &service.OpsService{})
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/list", h.List)
	r.GET("/stats", h.Stats)
	r.GET("/cache", h.CacheHitRate)
	start := time.Now().UTC().Truncate(time.Minute).Add(-3 * time.Hour)
	end := start.Add(3 * time.Hour)
	q := url.Values{"start_time": {start.Format(time.RFC3339)}, "end_time": {end.Format(time.RFC3339)}, "timezone": {"UTC"}}
	get := func(path string, query url.Values) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path+"?"+query.Encode(), nil))
		return w
	}
	require.Equal(t, 200, get("/list", q).Code)
	require.True(t, repo.listFilters.StartTime.Equal(start))
	require.True(t, repo.listFilters.EndTime.Equal(end))
	require.Equal(t, 200, get("/stats", q).Code)
	require.True(t, repo.statsFilters.StartTime.Equal(start))
	require.True(t, repo.statsFilters.EndTime.Equal(end))
	q.Set("dimension", "account")
	q.Set("include_performance", "1")
	w := get("/cache", q)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 1, repo.perfCalls)
	require.True(t, repo.errorRows)
	var result struct {
		Data usagestats.AccountPerformanceResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Equal(t, "available", result.Data.ErrorState)
	require.NotEmpty(t, result.Data.WindowStart)
	q.Set("request_type", "stream")
	w = get("/cache", q)
	require.Equal(t, 200, w.Code)
	require.False(t, repo.errorRows)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Equal(t, "unsupported_filters", result.Data.ErrorState)
	h.opsService = service.NewOpsService(nil, nil, &config.Config{}, nil, nil, nil, nil, nil, nil, nil, nil)
	w = get("/cache", q)
	require.Equal(t, 200, w.Code)
	require.False(t, repo.errorRows)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Equal(t, "monitoring_disabled", result.Data.ErrorState)
	before := repo.perfCalls
	request := httptest.NewRequest("GET", "/cache?"+q.Encode(), nil)
	request = request.WithContext(adminaccess.WithPrincipal(request.Context(), &adminaccess.Principal{Policy: adminaccess.Policy{GlobalPermissions: []string{"usage.read"}}}))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, request)
	require.Equal(t, 403, w.Code)
	require.Equal(t, before, repo.perfCalls)
	q.Del("request_type")
	q.Del("include_performance")
	w = get("/cache", q)
	require.Equal(t, 200, w.Code)
	require.Equal(t, 1, repo.calls)
	q.Set("start_date", start.Format("2006-01-02"))
	require.Equal(t, 400, get("/list", q).Code)
	require.Equal(t, 400, get("/stats", q).Code)
	require.Equal(t, 400, get("/cache", q).Code)
}
