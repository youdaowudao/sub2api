package admin

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type cacheRateRepoCapture struct {
	adminUsageRepoCapture
	calls   int
	filters usagestats.UsageLogFilters
	ids     []int64
}

func (s *cacheRateRepoCapture) GetCacheHitRateStats(ctx context.Context, filters usagestats.UsageLogFilters, dimension string, ids []int64) ([]usagestats.CacheHitRateItem, error) {
	s.calls++
	s.filters = filters
	s.ids = ids
	return []usagestats.CacheHitRateItem{{Requests: 2, InputTokens: 10, CacheCreationTokens: 10, CacheReadTokens: 80, CacheHitRate: usagestats.CalculateCacheHitRate(10, 10, 80)}}, nil
}
func TestAdminCacheHitRateValidationAndCaching(t *testing.T) {
	usageCacheHitRateCache = newSnapshotCache(30 * time.Second)
	t.Cleanup(func() { usageCacheHitRateCache = newSnapshotCache(30 * time.Second) })
	repo := &cacheRateRepoCapture{}
	h := NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), nil, nil, nil)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/cache", h.CacheHitRate)
	r.GET("/stats", h.Stats)
	today := time.Now().UTC()
	y := today.AddDate(0, 0, -1).Format("2006-01-02")
	d := today.Format("2006-01-02")
	q := url.Values{"dimension": {"total"}, "start_date": {y}, "end_date": {d}, "timezone": {"UTC"}}
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	first := get("/cache?" + q.Encode())
	require.Equal(t, 200, first.Code)
	require.Equal(t, 1, repo.calls)
	q.Set("start_date", today.AddDate(0, 0, -6).Format("2006-01-02"))
	second := get("/cache?" + q.Encode())
	require.Equal(t, first.Body.String(), second.Body.String())
	require.Equal(t, 1, repo.calls)
	var body map[string]any
	require.NoError(t, json.Unmarshal(second.Body.Bytes(), &body))
	data, ok := body["data"].(map[string]any)
	require.True(t, ok)
	require.NotContains(t, data, "range")
	require.NotContains(t, data, "clamped")
	q.Set("dimension", "group")
	require.Equal(t, 200, get("/cache?"+q.Encode()).Code)
	require.Equal(t, 2, repo.calls)
	q.Set("group_id", "9")
	require.Equal(t, 200, get("/cache?"+q.Encode()).Code)
	require.Equal(t, 3, repo.calls)
	q.Set("nocache", "1")
	get("/cache?" + q.Encode())
	get("/cache?" + q.Encode())
	require.Equal(t, 5, repo.calls)
	for _, extra := range []string{"dimension=invalid", "dimension=total", "dimension=total&start_date=bad&end_date=" + d, "dimension=group&start_date=" + y + "&end_date=" + d + "&account_ids=1", "dimension=account&start_date=" + y + "&end_date=" + d + "&account_ids=no", "dimension=account&start_date=" + y + "&end_date=" + d + "&account_ids=" + strings.Repeat("1,", 1000) + "1"} {
		require.Equal(t, 400, get("/cache?"+extra).Code, extra)
	}
	q.Set("dimension", "account")
	q.Set("account_ids", "3,1,3")
	q.Set("user_id", "42")
	q.Set("api_key_id", "4")
	q.Set("account_id", "5")
	q.Set("model", "fixture")
	q.Set("billing_mode", "token")
	q.Set("request_type", "ws_v2")
	q.Set("native_compaction_v2", "true")
	q.Set("billing_type", "1")
	q.Set("upstream_model_mismatch", "true")
	require.Equal(t, 200, get("/cache?"+q.Encode()).Code)
	captured := repo.filters
	require.Equal(t, 200, get("/stats?"+q.Encode()).Code)
	captured.StartTime = nil
	captured.EndTime = nil
	repo.statsFilters.StartTime = nil
	repo.statsFilters.EndTime = nil
	require.Equal(t, repo.statsFilters, captured)
	require.Equal(t, []int64{1, 3}, repo.ids)
}

func TestUsageCacheHitRateCacheKey(t *testing.T) {
	f := usagestats.UsageLogFilters{}
	base := usageCacheHitRateCacheKey("account", f, []int64{3, 1, 3})
	require.Equal(t, base, usageCacheHitRateCacheKey("account", f, []int64{1, 3}))
	require.NotEqual(t, base, usageCacheHitRateCacheKey("account", f, []int64{1, 4}))
	require.NotEqual(t, base, usageCacheHitRateCacheKey("group", f, []int64{1, 3}))
	for _, changed := range []usagestats.UsageLogFilters{{GroupID: 1}, {Model: "m"}, {UserID: 1}, {APIKeyID: 1}, {AccountID: 1}, {BillingMode: "token"}} {
		require.NotEqual(t, base, usageCacheHitRateCacheKey("account", changed, []int64{1, 3}))
	}
}

func TestCacheHitRateSettingsSave(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})
	w := doUpdateSettings(t, h, map[string]any{"allow_user_view_cache_hit_rate": true, "cache_hit_rate_default_range": "yesterday"}, nil)
	require.Equal(t, 200, w.Code)
	require.Equal(t, "true", repo.values["allow_user_view_cache_hit_rate"])
	require.Equal(t, "yesterday", repo.values["cache_hit_rate_default_range"])
	w = doUpdateSettings(t, h, map[string]any{"cache_hit_rate_default_range": "week"}, nil)
	require.Equal(t, 400, w.Code)
	require.Equal(t, "yesterday", repo.values["cache_hit_rate_default_range"])
	w = doUpdateSettings(t, h, map[string]any{"site_name": "Fixture"}, nil)
	require.Equal(t, 200, w.Code)
	require.Equal(t, "true", repo.values["allow_user_view_cache_hit_rate"])
	require.Equal(t, "yesterday", repo.values["cache_hit_rate_default_range"])
}

func TestCacheHitRateSettingsAudit(t *testing.T) {
	changed := diffSettings(&service.SystemSettings{}, &service.SystemSettings{
		AllowUserViewCacheHitRate: true, CacheHitRateDefaultRange: "yesterday",
	}, nil, nil, UpdateSettingsRequest{})
	require.Contains(t, changed, "allow_user_view_cache_hit_rate")
	require.Contains(t, changed, "cache_hit_rate_default_range")
}
