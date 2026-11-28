package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type dashboardPreciseRepo struct {
	dashboardUsageRepoCapture
	minuteCalls, oldTrendCalls, modelCalls, groupCalls, rankingCalls int
	rankingStart, rankingEnd                                         time.Time
	rankingResponse                                                  *usagestats.UserSpendingRankingResponse
	minuteQuery, modelQuery, groupQuery                              dashboardPreciseQuery
	breakdownCalls                                                   int
	breakdownStart, breakdownEnd                                     time.Time
	breakdownDim                                                     usagestats.UserBreakdownDimension
	breakdownLimit                                                   int
	modelSource                                                      string
	statsCalls                                                       int
	dashboardStats                                                   *usagestats.DashboardStats
}

func (r *dashboardPreciseRepo) GetDashboardStats(_ context.Context) (*usagestats.DashboardStats, error) {
	r.statsCalls++
	return r.dashboardStats, nil
}

func (r *dashboardPreciseRepo) GetDashboardStatsWithRange(ctx context.Context, _, _ time.Time) (*usagestats.DashboardStats, error) {
	return r.GetDashboardStats(ctx)
}

type dashboardPreciseQuery struct {
	start, end time.Time
	filters    usagestats.UsageLogFilters
}

func (r *dashboardPreciseRepo) GetMinuteUsageTrendWithUsageFilters(_ context.Context, start, end time.Time, filters usagestats.UsageLogFilters) ([]usagestats.TrendDataPoint, error) {
	r.minuteCalls++
	r.minuteQuery = dashboardPreciseQuery{start, end, filters}
	return []usagestats.TrendDataPoint{{Date: "2026-11-27T00:37:00Z", Requests: 7}}, nil
}

func (r *dashboardPreciseRepo) GetUsageTrendWithUsageFilters(_ context.Context, _, _ time.Time, _ string, _ usagestats.UsageLogFilters) ([]usagestats.TrendDataPoint, error) {
	r.oldTrendCalls++
	return []usagestats.TrendDataPoint{{Date: "2026-11-27", Requests: 1}}, nil
}

func (r *dashboardPreciseRepo) GetModelStatsWithUsageFiltersBySource(_ context.Context, start, end time.Time, filters usagestats.UsageLogFilters, source string) ([]usagestats.ModelStat, error) {
	r.modelCalls++
	r.modelQuery = dashboardPreciseQuery{start, end, filters}
	r.modelSource = source
	return []usagestats.ModelStat{}, nil
}

func (r *dashboardPreciseRepo) GetGroupStatsWithUsageFilters(_ context.Context, start, end time.Time, filters usagestats.UsageLogFilters) ([]usagestats.GroupStat, error) {
	r.groupCalls++
	r.groupQuery = dashboardPreciseQuery{start, end, filters}
	return []usagestats.GroupStat{}, nil
}

func (r *dashboardPreciseRepo) GetUserUsageTrend(_ context.Context, _, _ time.Time, _ string, _ int, _ string) ([]usagestats.UserUsageTrendPoint, error) {
	return []usagestats.UserUsageTrendPoint{}, nil
}

func (r *dashboardPreciseRepo) GetUserSpendingRanking(_ context.Context, start, end time.Time, limit int) (*usagestats.UserSpendingRankingResponse, error) {
	r.rankingCalls++
	r.rankingStart, r.rankingEnd = start, end
	r.rankingLimit = limit
	if r.rankingResponse != nil {
		return r.rankingResponse, nil
	}
	return &usagestats.UserSpendingRankingResponse{Ranking: []usagestats.UserSpendingRankingItem{}}, nil
}

func (r *dashboardPreciseRepo) GetUserBreakdownStats(_ context.Context, start, end time.Time, dim usagestats.UserBreakdownDimension, limit int) ([]usagestats.UserBreakdownItem, error) {
	r.breakdownCalls++
	r.breakdownStart, r.breakdownEnd = start, end
	r.breakdownDim, r.breakdownLimit = dim, limit
	return []usagestats.UserBreakdownItem{}, nil
}

func newPreciseDashboardRouter(t *testing.T) (*gin.Engine, *dashboardPreciseRepo) {
	t.Helper()
	reset := func() {
		resetDashboardReadCachesForTest()
		dashboardUsersRankingCache = newSnapshotCache(5 * time.Minute)
	}
	reset()
	t.Cleanup(reset)
	gin.SetMode(gin.TestMode)
	repo := &dashboardPreciseRepo{}
	h := NewDashboardHandler(service.NewDashboardService(repo, nil, nil, nil), nil)
	router := gin.New()
	router.GET("/snapshot", h.GetSnapshotV2)
	router.GET("/stats", h.GetStats)
	router.GET("/ranking", h.GetUserSpendingRanking)
	router.GET("/breakdown", h.GetUserBreakdown)
	return router, repo
}

func preciseDashboardRequest(router *gin.Engine, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func preciseDashboardPayload(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.NotNil(t, body.Data)
	return body.Data
}

func TestDashboardRangePreciseParse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/?timezone=Asia/Tokyo&start_date=2026-11-26&end_date=2026-11-27", nil)
	dateStart, dateEnd := parseTimeRange(c)
	start, end, precise, err := parseDashboardRange(c)
	require.NoError(t, err)
	assert.False(t, precise)
	assert.Equal(t, dateStart, start)
	assert.Equal(t, dateEnd, end)

	// Explicit time parameters take priority over date parameters; offsets are
	// parsed as instants and the result returned in the requested timezone.
	c, _ = gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/?timezone=Asia/Tokyo&start_date=2026-11-20&end_date=2026-11-22&start_time=2026-11-26T14:00:00Z&end_time=2026-11-27T00:00:00%2B09:00", nil)
	start, end, precise, err = parseDashboardRange(c)
	require.NoError(t, err)
	assert.True(t, precise)
	assert.Equal(t, "Asia/Tokyo", start.Location().String())
	assert.Equal(t, "Asia/Tokyo", end.Location().String())
	assert.Equal(t, "2026-11-26T23:00:00+09:00", start.Format(time.RFC3339))
	assert.Equal(t, "2026-11-27T00:00:00+09:00", end.Format(time.RFC3339))
}

func TestDashboardRangePreciseParseInvalid(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ name, query string }{
		{"start only", "start_time=2026-11-27T00:00:00Z"},
		{"end only", "end_time=2026-11-27T01:00:00Z"},
		{"both empty", "start_time=&end_time="},
		{"invalid start", "start_time=bad&end_time=2026-11-27T01:00:00Z"},
		{"invalid end", "start_time=2026-11-27T00:00:00Z&end_time=bad"},
		{"equal", "start_time=2026-11-27T01:00:00Z&end_time=2026-11-27T01:00:00Z"},
		{"reversed", "start_time=2026-11-27T02:00:00Z&end_time=2026-11-27T01:00:00Z"},
		{"seconds", "start_time=2026-11-27T00:00:01Z&end_time=2026-11-27T01:00:00Z"},
		{"nanoseconds", "start_time=2026-11-27T00:00:00Z&end_time=2026-11-27T01:00:00.000000001Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/?"+tc.query, nil)
			_, _, _, err := parseDashboardRange(c)
			require.EqualError(t, err, "invalid start_time/end_time")
		})
	}
}

func TestDashboardRangePreciseSnapshot(t *testing.T) {
	router, repo := newPreciseDashboardRouter(t)
	path := "/snapshot?include_stats=false&include_group_stats=true&timezone=UTC&start_time=2026-11-27T00:37:00Z&end_time=2026-11-27T01:37:00Z"
	for i := 0; i < 2; i++ {
		rec := preciseDashboardRequest(router, path)
		payload := preciseDashboardPayload(t, rec)
		assert.Equal(t, "minute", payload["granularity"])
		assert.Equal(t, "2026-11-27", payload["start_date"])
		assert.Equal(t, "2026-11-27", payload["end_date"])
		assert.Equal(t, "miss", rec.Header().Get("X-Snapshot-Cache"))
		assert.NotEmpty(t, rec.Header().Get("ETag"))
		assert.Equal(t, "If-None-Match", rec.Header().Get("Vary"))
		assert.Contains(t, rec.Body.String(), "2026-11-27T00:37:00Z")
	}
	assert.Equal(t, 2, repo.minuteCalls)
	assert.Zero(t, repo.oldTrendCalls)
	assert.Equal(t, 2, repo.modelCalls)
	assert.Equal(t, 2, repo.groupCalls)
	for name, cache := range map[string]*snapshotCache{
		"snapshot": dashboardSnapshotV2Cache,
		"trend":    dashboardTrendCache,
		"model":    dashboardModelStatsCache,
		"group":    dashboardGroupStatsCache,
		"ranking":  dashboardUsersRankingCache,
	} {
		assert.Empty(t, cache.items, name)
	}
}

func TestDashboardRangePreciseRejectsInvalidWindow(t *testing.T) {
	for _, tc := range []struct{ name, query string }{
		{"over three hours", "start_time=2026-11-27T00:00:00Z&end_time=2026-11-27T03:01:00Z"},
		{"start only", "start_time=2026-11-27T00:00:00Z"},
		{"end only", "end_time=2026-11-27T01:00:00Z"},
		{"invalid", "start_time=bad&end_time=2026-11-27T01:00:00Z"},
		{"empty window", "start_time=2026-11-27T01:00:00Z&end_time=2026-11-27T01:00:00Z"},
		{"non-minute", "start_time=2026-11-27T00:00:01Z&end_time=2026-11-27T01:00:00Z"},
		{"fractional minute", "start_time=2026-11-27T00:00:00Z&end_time=2026-11-27T01:00:00.5Z"},
		{"users trend", "start_time=2026-11-27T00:00:00Z&end_time=2026-11-27T01:00:00Z&include_users_trend=true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router, repo := newPreciseDashboardRouter(t)
			rec := preciseDashboardRequest(router, "/snapshot?include_stats=false&include_model_stats=false&"+tc.query)
			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.Zero(t, repo.minuteCalls)
			assert.Zero(t, repo.oldTrendCalls)
		})
	}
}

func TestDashboardRangePreciseSnapshotMidnight(t *testing.T) {
	router, _ := newPreciseDashboardRouter(t)
	for _, query := range []string{
		"timezone=UTC&start_time=2026-11-26T23:00:00Z&end_time=2026-11-27T00:00:00Z",
		"timezone=Asia/Tokyo&start_time=2026-11-26T14:00:00Z&end_time=2026-11-26T15:00:00Z",
	} {
		payload := preciseDashboardPayload(t, preciseDashboardRequest(router, "/snapshot?include_stats=false&"+query))
		assert.Equal(t, "2026-11-26", payload["start_date"])
		assert.Equal(t, "2026-11-26", payload["end_date"])
	}
}

func TestDashboardRangePreciseSnapshotFiltersAndThreeHourLimit(t *testing.T) {
	router, repo := newPreciseDashboardRouter(t)
	path := "/snapshot?include_stats=false&include_group_stats=true&timezone=UTC&start_time=2026-11-27T00:00:00Z&end_time=2026-11-27T03:00:00Z&granularity=hour&user_id=1&api_key_id=2&account_id=3&group_id=4&model=test-model&request_type=stream&native_compaction_v2=true&billing_type=1&upstream_model_mismatch=false"
	payload := preciseDashboardPayload(t, preciseDashboardRequest(router, path))
	assert.Equal(t, "minute", payload["granularity"])
	assert.Equal(t, 1, repo.minuteCalls)
	assert.Zero(t, repo.oldTrendCalls)
	assert.Equal(t, 1, repo.modelCalls)
	assert.Equal(t, 1, repo.groupCalls)
	requestType := int16(service.RequestTypeStream)
	billingType := int8(1)
	native, mismatch := true, false
	filters := usagestats.UsageLogFilters{
		UserID: 1, APIKeyID: 2, AccountID: 3, GroupID: 4,
		RequestType: &requestType, NativeCompactionV2: &native,
		BillingType: &billingType, UpstreamModelMismatch: &mismatch,
	}
	start := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	end := start.Add(3 * time.Hour)
	assert.Equal(t, dashboardPreciseQuery{start, end, filters}, repo.modelQuery)
	assert.Equal(t, dashboardPreciseQuery{start, end, filters}, repo.groupQuery)
	assert.Equal(t, usagestats.ModelSourceRequested, repo.modelSource)
	filters.Model = "test-model"
	assert.Equal(t, dashboardPreciseQuery{start, end, filters}, repo.minuteQuery)
}

func TestDashboardRangePreciseDateModeUnchanged(t *testing.T) {
	for _, tc := range []struct{ input, want string }{{"hour", "hour"}, {"day", "day"}, {"minute", "day"}, {"invalid", "day"}} {
		t.Run(tc.input, func(t *testing.T) {
			router, repo := newPreciseDashboardRouter(t)
			path := "/snapshot?include_stats=false&include_group_stats=true&include_users_trend=true&timezone=UTC&start_date=2026-11-26&end_date=2026-11-27&granularity=" + tc.input
			for _, cache := range []string{"miss", "hit"} {
				rec := preciseDashboardRequest(router, path)
				payload := preciseDashboardPayload(t, rec)
				assert.Equal(t, tc.want, payload["granularity"])
				assert.Equal(t, "2026-11-26", payload["start_date"])
				assert.Equal(t, "2026-11-27", payload["end_date"])
				assert.Equal(t, cache, rec.Header().Get("X-Snapshot-Cache"))
			}
			assert.Equal(t, 1, repo.oldTrendCalls)
			assert.Zero(t, repo.minuteCalls)
			assert.Equal(t, 1, repo.modelCalls)
			assert.Equal(t, 1, repo.groupCalls)
			assert.Len(t, dashboardSnapshotV2Cache.items, 1)
			assert.Len(t, dashboardTrendCache.items, 1)
			assert.Len(t, dashboardModelStatsCache.items, 1)
			assert.Len(t, dashboardGroupStatsCache.items, 1)
		})
	}
}

func TestUserSpendingRankingPrecise(t *testing.T) {
	router, repo := newPreciseDashboardRouter(t)
	path := "/ranking?timezone=UTC&start_time=2026-11-27T00:37:00Z&end_time=2026-11-27T01:37:00Z&limit=12"
	for i := 0; i < 2; i++ {
		rec := preciseDashboardRequest(router, path)
		payload := preciseDashboardPayload(t, rec)
		assert.Contains(t, payload, "total_users")
		assert.Equal(t, float64(0), payload["total_users"])
		assert.Equal(t, "2026-11-27", payload["start_date"])
		assert.Equal(t, "2026-11-27", payload["end_date"])
		assert.Equal(t, "miss", rec.Header().Get("X-Snapshot-Cache"))
	}
	assert.True(t, repo.rankingStart.Equal(time.Date(2026, 11, 27, 0, 37, 0, 0, time.UTC)), "ranking start=%s", repo.rankingStart)
	assert.True(t, repo.rankingEnd.Equal(time.Date(2026, 11, 27, 1, 37, 0, 0, time.UTC)), "ranking end=%s", repo.rankingEnd)
	assert.Equal(t, 2, repo.rankingCalls)
	assert.Empty(t, dashboardUsersRankingCache.items)
}

func TestUserSpendingRankingPreciseTotalUsersAndMidnight(t *testing.T) {
	router, repo := newPreciseDashboardRouter(t)
	repo.rankingResponse = &usagestats.UserSpendingRankingResponse{
		Ranking:    []usagestats.UserSpendingRankingItem{{UserID: 1}},
		TotalUsers: 24,
	}
	path := "/ranking?timezone=Asia/Tokyo&start_time=2026-11-26T13:00:00Z&end_time=2026-11-26T15:00:00Z&limit=1"
	payload := preciseDashboardPayload(t, preciseDashboardRequest(router, path))
	assert.Equal(t, float64(24), payload["total_users"])
	assert.Len(t, payload["ranking"], 1)
	assert.Equal(t, 1, repo.rankingLimit)
	assert.Equal(t, "2026-11-26", payload["start_date"])
	assert.Equal(t, "2026-11-26", payload["end_date"])
	assert.Equal(t, "2026-11-26T22:00:00+09:00", repo.rankingStart.Format(time.RFC3339))
	assert.Equal(t, "2026-11-27T00:00:00+09:00", repo.rankingEnd.Format(time.RFC3339))
}

func TestUserSpendingRankingPreciseDateModeUnchanged(t *testing.T) {
	router, repo := newPreciseDashboardRouter(t)
	path := "/ranking?timezone=UTC&start_date=2026-11-26&end_date=2026-11-27&limit=100"
	for _, expected := range []string{"miss", "hit"} {
		rec := preciseDashboardRequest(router, path)
		payload := preciseDashboardPayload(t, rec)
		assert.Equal(t, "2026-11-26", payload["start_date"])
		assert.Equal(t, "2026-11-27", payload["end_date"])
		assert.Equal(t, expected, rec.Header().Get("X-Snapshot-Cache"))
	}
	assert.Equal(t, 50, repo.rankingLimit)
	assert.Equal(t, 1, repo.rankingCalls)
	assert.True(t, repo.rankingStart.Equal(time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)))
	assert.True(t, repo.rankingEnd.Equal(time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC)))
	assert.Len(t, dashboardUsersRankingCache.items, 1)
}

func TestGetUserBreakdownPreciseWindow(t *testing.T) {
	router, repo := newPreciseDashboardRouter(t)
	path := "/breakdown?timezone=Asia/Tokyo&start_date=2026-11-01&end_date=2026-11-15&start_time=2026-11-26T13:00:00Z&end_time=2026-11-26T15:00:00Z&model=test-model&model_source=upstream&group_id=42&limit=2"
	payload := preciseDashboardPayload(t, preciseDashboardRequest(router, path))
	assert.Equal(t, "2026-11-26", payload["start_date"])
	assert.Equal(t, "2026-11-26", payload["end_date"])
	assert.Equal(t, "2026-11-26T22:00:00+09:00", repo.breakdownStart.Format(time.RFC3339))
	assert.Equal(t, "2026-11-27T00:00:00+09:00", repo.breakdownEnd.Format(time.RFC3339))
	assert.Equal(t, "test-model", repo.breakdownDim.Model)
	assert.Equal(t, usagestats.ModelSourceUpstream, repo.breakdownDim.ModelType)
	assert.Equal(t, int64(42), repo.breakdownDim.GroupID)
	assert.Equal(t, 2, repo.breakdownLimit)
	assert.Equal(t, 1, repo.breakdownCalls)
}

func TestDashboardRangePreciseRankingAndBreakdownRejectInvalidWindow(t *testing.T) {
	for _, path := range []string{"/ranking", "/breakdown"} {
		for _, tc := range []struct{ name, query string }{
			{"too long outside day start", "timezone=UTC&start_time=2026-11-27T00:01:00Z&end_time=2026-11-27T03:02:00Z"},
			{"start only", "start_time=2026-11-27T00:00:00Z"},
			{"end only", "end_time=2026-11-27T01:00:00Z"},
			{"invalid minute", "start_time=2026-11-27T00:00:01Z&end_time=2026-11-27T01:00:00Z"},
		} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				router, repo := newPreciseDashboardRouter(t)
				rec := preciseDashboardRequest(router, path+"?"+tc.query)
				assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
				assert.Contains(t, rec.Body.String(), "invalid start_time/end_time")
				assert.Zero(t, repo.rankingCalls)
				assert.Zero(t, repo.breakdownCalls)
				assert.Empty(t, dashboardUsersRankingCache.items)
			})
		}
	}
}

func TestDashboardRankingTodayYesterdayLongWindows(t *testing.T) {
	for _, tz := range []string{"Asia/Tokyo", "America/New_York"} {
		t.Run(tz, func(t *testing.T) {
			loc, err := time.LoadLocation(tz)
			require.NoError(t, err)
			now := time.Now().In(loc)
			today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
			yesterday := time.Date(now.Year(), now.Month(), now.Day()-1, 0, 0, 0, 0, loc)
			router, repo := newPreciseDashboardRouter(t)
			path := func(endpoint string, start, end time.Time, extra string) string {
				q := url.Values{"timezone": {tz}, "start_time": {start.Format(time.RFC3339)}, "end_time": {end.Format(time.RFC3339)}, "limit": {"50"}}
				return endpoint + "?" + q.Encode() + extra
			}
			end := yesterday.Add(5 * time.Hour)
			payload := preciseDashboardPayload(t, preciseDashboardRequest(router, path("/ranking", yesterday, end, "")))
			assert.Equal(t, yesterday.Format("2006-01-02"), payload["start_date"])
			assert.Equal(t, 1, repo.rankingCalls)
			assert.True(t, yesterday.Equal(repo.rankingStart))
			assert.True(t, end.Equal(repo.rankingEnd))
			assert.Equal(t, 50, repo.rankingLimit)
			assert.Empty(t, dashboardUsersRankingCache.items)
			if now.Sub(today) > 4*time.Hour {
				preciseDashboardPayload(t, preciseDashboardRequest(router, path("/ranking", today, today.Add(4*time.Hour), "")))
				assert.True(t, today.Equal(repo.rankingStart))
			}
			calls := repo.rankingCalls
			prior := time.Date(now.Year(), now.Month(), now.Day()-2, 0, 0, 0, 0, loc)
			for _, query := range []string{
				path("/ranking", yesterday.Add(time.Minute), end, ""),
				path("/ranking", yesterday, today.Add(time.Minute), ""),
				path("/ranking", prior, prior.Add(5*time.Hour), ""),
				path("/ranking", today, today.AddDate(0, 0, 1), ""),
				path("/ranking", yesterday, end, "&start_date="+yesterday.Format("2006-01-02")),
				path("/ranking", yesterday, end.Add(time.Second), ""),
				path("/snapshot", yesterday, end, ""),
				path("/breakdown", yesterday, end, ""),
			} {
				assert.Equal(t, http.StatusBadRequest, preciseDashboardRequest(router, query).Code, query)
			}
			assert.Equal(t, calls, repo.rankingCalls)
			assert.Zero(t, repo.breakdownCalls)
			assert.Zero(t, repo.minuteCalls)
		})
	}
}

func TestDashboardSnapshotConnectionUsage(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		cached      bool
	}{
		{"precise", "start_time=2026-11-27T00:37:00Z&end_time=2026-11-27T01:37:00Z", false},
		{"date", "start_date=2026-11-26&end_date=2026-11-27", true},
	} {
		for _, usage := range []*usagestats.DashboardConnectionUsage{nil, {WSUsers: 0, HTTPUsers: 2, WindowEnd: "2026-11-28T01:05:30Z"}, {WSUsers: 5, HTTPUsers: 6, WindowEnd: "2026-11-28T01:05:30Z"}} {
			t.Run(tc.name, func(t *testing.T) {
				router, repo := newPreciseDashboardRouter(t)
				repo.dashboardStats = &usagestats.DashboardStats{ConnectionUsage: usage, Rpm: 9}
				base := "/snapshot?include_trend=false&include_model_stats=false&include_group_stats=false&include_users_trend=false&timezone=UTC&group_id=7&" + tc.query
				got := preciseDashboardPayload(t, preciseDashboardRequest(router, base+"&include_stats=false"))
				require.NotContains(t, got, "stats")
				require.Zero(t, repo.statsCalls)
				for i := 0; i < 2; i++ {
					got = preciseDashboardPayload(t, preciseDashboardRequest(router, base+"&include_stats=true"))
					stats, ok := got["stats"].(map[string]any)
					require.True(t, ok)
					require.Equal(t, float64(9), stats["rpm"])
					if usage == nil {
						require.NotContains(t, stats, "connection_usage")
					} else {
						require.Equal(t, map[string]any{"ws_users": float64(usage.WSUsers), "http_users": float64(usage.HTTPUsers), "window_end": usage.WindowEnd}, stats["connection_usage"])
					}
				}
				want := 2
				if tc.cached {
					want = 1
				}
				require.Equal(t, want, repo.statsCalls)
				old := preciseDashboardPayload(t, preciseDashboardRequest(router, "/stats"))
				require.NotContains(t, old, "connection_usage")
				require.Equal(t, float64(9), old["rpm"])
				require.Equal(t, want+1, repo.statsCalls)
			})
		}
	}
}
