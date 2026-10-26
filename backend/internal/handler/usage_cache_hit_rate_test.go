package handler

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type cacheRateSettingRepo struct {
	service.SettingRepository
	enabled bool
	fail    bool
}

func (s *cacheRateSettingRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	if s.fail {
		return nil, errors.New("fixture unavailable")
	}
	v := "false"
	if s.enabled {
		v = "true"
	}
	return map[string]string{"allow_user_view_cache_hit_rate": v}, nil
}

type userCacheRateRepo struct {
	service.UsageLogRepository
	filters usagestats.UsageLogFilters
	calls   int
}

func (s *userCacheRateRepo) GetCacheHitRateStats(ctx context.Context, f usagestats.UsageLogFilters, dimension string, ids []int64) ([]usagestats.CacheHitRateItem, error) {
	s.filters = f
	s.calls++
	return []usagestats.CacheHitRateItem{{}}, nil
}
func TestUserCacheHitRate(t *testing.T) {
	settings := &cacheRateSettingRepo{}
	repo := &userCacheRateRepo{}
	h := NewUsageHandler(service.NewUsageService(repo, nil, nil, nil), nil, nil, service.NewSettingService(settings, &config.Config{}))
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42}) })
	r.GET("/cache", h.CacheHitRate)
	get := func(q string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/cache?"+q, nil))
		return w
	}
	require.Equal(t, 403, get("").Code)
	require.Zero(t, repo.calls)
	settings.enabled = true
	settings.fail = true
	require.Equal(t, 403, get("").Code)
	settings.fail = false
	w := get("start_date=bad&end_date=bad")
	require.Equal(t, 400, w.Code)
	require.Contains(t, w.Body.String(), "start_date and end_date are required, format YYYY-MM-DD")
	w = get("start_date=2026-10-25&end_date=2026-10-26&user_id=99")
	require.Equal(t, 200, w.Code)
	require.Equal(t, int64(42), repo.filters.UserID)
	require.Equal(t, 1, repo.calls)
}
