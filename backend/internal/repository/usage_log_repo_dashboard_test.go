package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

func TestUsageLogRepositoryGetMinuteUsageTrendWithUsageFilters(t *testing.T) {
	start := time.Date(2026, 11, 27, 10, 5, 0, 0, time.FixedZone("UTC+9", 9*3600))
	end := start.Add(time.Hour)
	for _, tc := range []struct {
		name    string
		filters UsageLogFilters
	}{
		{name: "unfiltered"},
		{name: "filtered", filters: UsageLogFilters{GroupID: 7}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, mock, queries := newDashboardSQLRecorder(t)
			expectation := mock.ExpectQuery("minute trend")
			if tc.filters.GroupID > 0 {
				expectation.WithArgs(start, end, tc.filters.GroupID)
			} else {
				expectation.WithArgs(start, end)
			}
			expectation.WillReturnRows(sqlmock.NewRows([]string{
				"date", "requests", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "total_tokens", "cost", "actual_cost",
			}).
				AddRow("2026-11-27T01:05:00Z", 2, 100, 20, 15, 25, 160, 0.5, 0.4).
				AddRow("2026-11-27T01:07:00Z", 1, 5, 10, 0, 0, 15, 0.1, 0.08))

			got, err := repo.GetMinuteUsageTrendWithUsageFilters(context.Background(), start, end, tc.filters)
			require.NoError(t, err)
			require.Equal(t, []TrendDataPoint{
				{Date: "2026-11-27T01:05:00Z", Requests: 2, InputTokens: 100, OutputTokens: 20, CacheCreationTokens: 15, CacheReadTokens: 25, TotalTokens: 160, Cost: 0.5, ActualCost: 0.4},
				{Date: "2026-11-27T01:07:00Z", Requests: 1, InputTokens: 5, OutputTokens: 10, TotalTokens: 15, Cost: 0.1, ActualCost: 0.08},
			}, got)
			require.Len(t, *queries, 1)
			query := (*queries)[0]
			require.Contains(t, query, `TO_CHAR(date_trunc('minute', created_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD"T"HH24:MI:SS"Z"') as date`)
			require.Contains(t, query, "FROM usage_logs WHERE created_at >= $1 AND created_at < $2")
			require.Contains(t, query, "GROUP BY date_trunc('minute', created_at AT TIME ZONE 'UTC') ORDER BY date_trunc('minute', created_at AT TIME ZONE 'UTC') ASC")
			require.NotContains(t, query, "usage_dashboard_hourly")
			require.NotContains(t, query, "usage_dashboard_daily")
			if tc.filters.GroupID > 0 {
				require.Contains(t, query, "AND group_id = $3")
			} else {
				require.NotContains(t, query, "AND group_id")
			}
		})
	}
}

func TestUsageLogRepositoryGetUserSpendingRankingTotalUsers(t *testing.T) {
	start := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	for _, tc := range []struct {
		name       string
		totalUsers int64
	}{
		{name: "more users than limit", totalUsers: 25},
		{name: "empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, mock, queries := newDashboardSQLRecorder(t)
			rows := sqlmock.NewRows([]string{
				"user_id", "email", "username", "actual_cost", "requests", "tokens", "total_actual_cost", "total_requests", "total_tokens", "total_users",
			})
			if tc.totalUsers > 0 {
				rows.AddRow(1, "user@example.com", "user", 10.5, 20, 100, 50.0, 100, 500, tc.totalUsers)
			}
			mock.ExpectQuery("ranking").WithArgs(start, end, 1).WillReturnRows(rows)
			got, err := repo.GetUserSpendingRanking(context.Background(), start, end, 1)
			require.NoError(t, err)
			require.Equal(t, tc.totalUsers, got.TotalUsers)
			require.Len(t, *queries, 1)
			require.Contains(t, (*queries)[0], "COUNT(*) OVER () as total_users FROM user_spend ORDER BY actual_cost DESC, tokens DESC, user_id ASC LIMIT $3")
			if tc.totalUsers > 0 {
				require.Len(t, got.Ranking, 1)
				require.Equal(t, 50.0, got.TotalActualCost)
				require.Equal(t, int64(100), got.TotalRequests)
				require.Equal(t, int64(500), got.TotalTokens)
			} else {
				require.NotNil(t, got.Ranking)
				require.Empty(t, got.Ranking)
				require.Zero(t, got.TotalActualCost)
				require.Zero(t, got.TotalRequests)
				require.Zero(t, got.TotalTokens)
			}
		})
	}
}

func TestUsageLogRepositoryDashboardTodayAverageDurationAggregated(t *testing.T) {
	start := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	now := start.Add(time.Hour)
	for _, tc := range []struct {
		name          string
		todayRequests int64
		todayDuration int64
		hasDailyRow   bool
	}{
		{name: "today has requests", todayRequests: 2, todayDuration: 251, hasDailyRow: true},
		{name: "today has no requests", hasDailyRow: true},
		{name: "today has no daily rollup"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newSQLMock(t)
			repo := &usageLogRepository{sql: db}
			mock.ExpectQuery("FROM usage_dashboard_daily").
				WithArgs().
				WillReturnRows(sqlmock.NewRows([]string{
					"total_requests", "total_input_tokens", "total_output_tokens", "total_cache_creation_tokens", "total_cache_read_tokens", "total_cost", "total_actual_cost", "total_account_cost", "total_duration_ms",
				}).AddRow(5, 10, 15, 20, 25, 1.0, 0.9, 0.8, 500))
			todayRows := sqlmock.NewRows([]string{
				"today_requests", "today_input_tokens", "today_output_tokens", "today_cache_creation_tokens", "today_cache_read_tokens", "today_cost", "today_actual_cost", "today_account_cost", "active_users", "total_duration_ms",
			})
			if tc.hasDailyRow {
				todayRows.AddRow(tc.todayRequests, 0, 0, 0, 0, 0.0, 0.0, 0.0, 0, tc.todayDuration)
			}
			mock.ExpectQuery("FROM usage_dashboard_daily WHERE bucket_date =").
				WithArgs(start).
				WillReturnRows(todayRows)
			mock.ExpectQuery("FROM usage_dashboard_hourly").
				WithArgs(now.In(timezone.Location()).Truncate(time.Hour)).
				WillReturnRows(sqlmock.NewRows([]string{"active_users"}).AddRow(0))

			stats := &DashboardStats{}
			require.NoError(t, repo.fillDashboardUsageStatsAggregated(context.Background(), stats, start, now))
			require.Equal(t, int64(5), stats.TotalRequests)
			require.Equal(t, 100.0, stats.AverageDurationMs)
			require.Equal(t, tc.todayRequests, stats.TodayRequests)
			if tc.todayRequests > 0 {
				require.NotNil(t, stats.TodayAverageDurationMs)
				require.Equal(t, 125.5, *stats.TodayAverageDurationMs)
			} else {
				require.Nil(t, stats.TodayAverageDurationMs)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestUsageLogRepositoryDashboardTodayAverageDurationFromUsageLogs(t *testing.T) {
	start := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	now := start.Add(time.Hour)
	for _, tc := range []struct {
		name          string
		todayRequests int64
		todayDuration int64
	}{
		{name: "today has requests", todayRequests: 2, todayDuration: 251},
		{name: "today has no requests"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := newSQLMock(t)
			repo := &usageLogRepository{sql: db}
			mock.ExpectQuery("WITH scoped AS").
				WithArgs(start.Add(-24*time.Hour), now, start, start.Add(24*time.Hour)).
				WillReturnRows(sqlmock.NewRows([]string{
					"total_requests", "total_input_tokens", "total_output_tokens", "total_cache_creation_tokens", "total_cache_read_tokens", "total_cost", "total_actual_cost", "total_account_cost", "total_duration_ms",
					"today_requests", "today_input_tokens", "today_output_tokens", "today_cache_creation_tokens", "today_cache_read_tokens", "today_cost", "today_actual_cost", "today_account_cost", "today_duration_ms",
				}).AddRow(5, 10, 15, 20, 25, 1.0, 0.9, 0.8, 500, tc.todayRequests, 0, 0, 0, 0, 0.0, 0.0, 0.0, tc.todayDuration))
			mock.ExpectQuery("WITH scoped AS").
				WithArgs(start, start.Add(24*time.Hour), now, now.Add(time.Hour)).
				WillReturnRows(sqlmock.NewRows([]string{"active_users", "hourly_active_users"}).AddRow(0, 0))

			stats := &DashboardStats{}
			require.NoError(t, repo.fillDashboardUsageStatsFromUsageLogs(context.Background(), stats, start.Add(-24*time.Hour), now, start, now))
			require.Equal(t, int64(5), stats.TotalRequests)
			require.Equal(t, 100.0, stats.AverageDurationMs)
			require.Equal(t, tc.todayRequests, stats.TodayRequests)
			if tc.todayRequests > 0 {
				require.NotNil(t, stats.TodayAverageDurationMs)
				require.Equal(t, 125.5, *stats.TodayAverageDurationMs)
			} else {
				require.Nil(t, stats.TodayAverageDurationMs)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
