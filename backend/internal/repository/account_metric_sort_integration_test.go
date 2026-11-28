//go:build integration

package repository

import (
	"reflect"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (s *AccountRepoSuite) TestMetricSortBeforePagination() {
	user := mustCreateUser(s.T(), s.client, &service.User{Email: "metric-sort@example.test"})
	key := mustCreateApiKey(s.T(), s.client, &service.APIKey{UserID: user.ID})
	group := mustCreateGroup(s.T(), s.client, &service.Group{Name: "metric-group"})
	a := mustCreateAccount(s.T(), s.client, &service.Account{Name: "metric-a"})
	b := mustCreateAccount(s.T(), s.client, &service.Account{Name: "metric-b"})
	c := mustCreateAccount(s.T(), s.client, &service.Account{Name: "metric-c"})
	d := mustCreateAccount(s.T(), s.client, &service.Account{Name: "metric-d"})
	e := mustCreateAccount(s.T(), s.client, &service.Account{Name: "metric-no-log"})
	f := mustCreateAccount(s.T(), s.client, &service.Account{Name: "metric-negative"})
	g := mustCreateAccount(s.T(), s.client, &service.Account{Name: "metric-null"})
	outside := mustCreateAccount(s.T(), s.client, &service.Account{Name: "metric-outside"})
	ids := []int64{a.ID, b.ID, c.ID, d.ID, e.ID, f.ID, g.ID}
	for _, id := range ids {
		s.Require().NoError(s.repo.AddToGroup(s.ctx, id, group.ID, 1))
	}
	start := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	logs := newUsageLogRepositoryWithSQL(s.client, s.repo.sql)
	insert := func(id int64, input, cache int, cost, userCost float64, at time.Time) {
		_, err := logs.Create(s.ctx, &service.UsageLog{UserID: user.ID, APIKeyID: key.ID, AccountID: id, Model: "metric", InputTokens: input, CacheReadTokens: cache, TotalCost: cost, ActualCost: userCost, CreatedAt: at})
		s.Require().NoError(err)
	}
	// Four different accounts lead the four today metrics. The cache window is
	// [start,end), while the existing today aggregation has no upper bound.
	for i := 0; i < 3; i++ {
		insert(a.ID, 2, 8, 1, 1, start.Add(time.Duration(i)*time.Minute))
	}
	insert(b.ID, 800, 200, 2, 2, start)
	insert(c.ID, 80, 20, 20, 1, start)
	insert(d.ID, 100, 0, 1, 30, start)
	insert(f.ID, 110, -10, 0, 0, start)
	insert(g.ID, 0, 0, 0, 0, start)
	insert(a.ID, 10000, 0, 500, 500, start.Add(-time.Second))
	insert(d.ID, 1, 0, 0, 0, end)
	insert(outside.ID, 0, 10000, 500, 500, start)
	oldCache, err := logs.GetCacheHitRateStats(s.ctx, UsageLogFilters{StartTime: &start, EndTime: &end}, "account", ids)
	s.Require().NoError(err)
	oldCacheByID := map[int64]usagestats.CacheHitRateItem{}
	for _, row := range oldCache {
		oldCacheByID[row.ID] = row
	}
	oldToday, err := logs.GetAccountWindowStatsBatch(s.ctx, ids, start)
	s.Require().NoError(err)

	// RED for older timer tests copies repository tests onto the old package.
	// Resolve this new feature's method dynamically to keep those tests compilable.
	capture := &accountPerformanceSQLCapture{sqlExecutor: s.repo.sql}
	reader := newAccountRepositoryWithSQL(s.client, capture, nil)
	method := reflect.ValueOf(reader).MethodByName("ListWithMetricSort")
	s.Require().True(method.IsValid())
	load := func(key, order string, page int) reflect.Value {
		before := capture.queries
		started := time.Now()
		values := method.Call([]reflect.Value{
			reflect.ValueOf(s.ctx), reflect.ValueOf(pagination.PaginationParams{Page: page, PageSize: 2, SortBy: key, SortOrder: order}),
			reflect.ValueOf(""), reflect.ValueOf(""), reflect.ValueOf(""), reflect.ValueOf("metric-"),
			reflect.ValueOf(group.ID), reflect.ValueOf(""), reflect.ValueOf(int64(0)), reflect.ValueOf(start), reflect.ValueOf(end),
		})
		s.Require().True(values[1].IsNil(), "%v", values[1].Interface())
		s.Require().EqualValues(len(ids), values[0].FieldByName("Total").Int())
		elapsed := time.Since(started)
		expected := 1
		if (page-1)*2 >= len(ids) {
			expected = 0
		}
		s.Require().Equal(before+expected, capture.queries, "only one aggregate for a nonempty metric page")
		if page == 1 && order == "desc" {
			planRows, err := s.repo.sql.QueryContext(s.ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+capture.query, capture.args...)
			s.Require().NoError(err)
			defer func() { _ = planRows.Close() }()
			s.Require().True(planRows.Next())
			var plan []byte
			s.Require().NoError(planRows.Scan(&plan))
			s.Require().NoError(planRows.Close())
			s.T().Logf("metric-sort isolated fixture %s: aggregate reads=%d, elapsed/list=%s; plan=%s", key, expected, elapsed, plan)
		}
		return values[0]
	}
	cacheOrder := func(order string) []int64 {
		var sorted []int64
		for page := 1; page <= 4; page++ {
			result := load("cache_hit_rate", order, page)
			rows := result.FieldByName("Accounts").Interface().([]service.Account)
			cacheItems := result.FieldByName("CacheStats").Interface().(map[int64]usagestats.CacheHitRateItem)
			for _, row := range rows {
				if old, ok := oldCacheByID[row.ID]; ok {
					s.Require().Equal(old, cacheItems[row.ID])
				} else {
					s.Require().NotContains(cacheItems, row.ID)
				}
			}
			sorted = append(sorted, accountMetricTestIDs(rows)...)
		}
		return sorted
	}
	s.Require().Equal([]int64{a.ID, b.ID, c.ID, d.ID, f.ID, e.ID, g.ID}, cacheOrder("desc"))
	s.Require().Equal([]int64{f.ID, d.ID, b.ID, c.ID, a.ID, e.ID, g.ID}, cacheOrder("asc"))
	s.Require().Equal(float64(-10), *oldCacheByID[f.ID].CacheHitRate)
	s.Require().Nil(oldCacheByID[g.ID].CacheHitRate)
	s.Require().NotContains(oldCacheByID, e.ID)
	s.Require().EqualValues(3, oldToday[a.ID].Requests)
	s.Require().EqualValues(2, oldToday[d.ID].Requests)
	s.Require().EqualValues(1000, oldToday[b.ID].Tokens)
	for key, firstID := range map[string]int64{
		"today_requests": a.ID, "today_tokens": b.ID, "today_account_cost": c.ID, "today_user_cost": d.ID,
	} {
		for page := 1; page <= 4; page++ {
			result := load(key, "desc", page)
			rows := result.FieldByName("Accounts").Interface().([]service.Account)
			stats := result.FieldByName("TodayStats").Interface().(map[int64]*usagestats.AccountStats)
			if page == 1 {
				s.Require().Equal(firstID, rows[0].ID, key)
			}
			for _, row := range rows {
				s.Require().Equal(oldToday[row.ID], stats[row.ID])
			}
		}
	}
	s.Require().Empty(load("cache_hit_rate", "desc", 5).FieldByName("Accounts").Interface())

}

func accountMetricTestIDs(rows []service.Account) []int64 {
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}
