package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

// Capture actual SQL independently of the result shape: the synthetic error
// stops the query before scanning. Numerical results are covered separately.
func newDashboardSQLRecorder(t *testing.T) (*usageLogRepository, sqlmock.Sqlmock, *[]string) {
	t.Helper()
	var captured []string
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(func(_, actual string) error {
		captured = append(captured, strings.Join(strings.Fields(actual), " "))
		return nil
	})))
	require.NoError(t, err)
	t.Cleanup(func() {
		mock.ExpectClose()
		require.NoError(t, db.Close())
		require.NoError(t, mock.ExpectationsWereMet())
	})
	return &usageLogRepository{sql: db}, mock, &captured
}

func TestUsageLogRepositoryGetGroupStatsCacheColumnsSQL(t *testing.T) {
	repo, mock, queries := newDashboardSQLRecorder(t)
	start := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	stop := errors.New("capture SQL")
	mock.ExpectQuery("groups").WithArgs(start, start.Add(time.Hour)).WillReturnError(stop)
	_, err := repo.GetGroupStatsWithUsageFilters(context.Background(), start, start.Add(time.Hour), UsageLogFilters{})
	require.ErrorIs(t, err, stop)
	require.Len(t, *queries, 1)
	require.Contains(t, (*queries)[0], "SUM(ul.cache_read_tokens)")
	require.Contains(t, (*queries)[0], "SUM(ul.cache_creation_tokens)")
	require.Contains(t, (*queries)[0], "SUM(ul.input_tokens)")
}

func TestUsageLogRepositoryGetUserSpendingRankingTotalUsersSQL(t *testing.T) {
	repo, mock, queries := newDashboardSQLRecorder(t)
	start := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	stop := errors.New("capture SQL")
	mock.ExpectQuery("ranking").WithArgs(start, start.Add(time.Hour), 12).WillReturnError(stop)
	_, err := repo.GetUserSpendingRanking(context.Background(), start, start.Add(time.Hour), 12)
	require.ErrorIs(t, err, stop)
	require.Len(t, *queries, 1)
	require.Contains(t, (*queries)[0], "COUNT(*) OVER ()")
}

func TestUsageLogRepositoryMinuteTrendSQL(t *testing.T) {
	for _, entry := range []string{"usage filters", "filters"} {
		t.Run(entry, func(t *testing.T) {
			repo, mock, queries := newDashboardSQLRecorder(t)
			start := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
			stop := errors.New("capture SQL")
			mock.ExpectQuery("trend").WithArgs(start, start.Add(time.Hour)).WillReturnError(stop)
			var err error
			if entry == "usage filters" {
				_, err = repo.GetUsageTrendWithUsageFilters(context.Background(), start, start.Add(time.Hour), "minute", UsageLogFilters{})
			} else {
				_, err = repo.GetUsageTrendWithFilters(context.Background(), start, start.Add(time.Hour), "minute", 0, 0, 0, 0, "", nil, nil, nil)
			}
			require.ErrorIs(t, err, stop)
			require.Len(t, *queries, 1)
			require.Contains(t, (*queries)[0], "TO_CHAR(created_at, 'YYYY-MM-DD')")
			require.NotContains(t, (*queries)[0], "HH24:MI")
		})
	}
}

func TestUsageLogRepositoryDashboardTodayAverageDurationSQL(t *testing.T) {
	start := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	now := start.Add(time.Hour)
	t.Run("aggregated", func(t *testing.T) {
		repo, mock, queries := newDashboardSQLRecorder(t)
		stop := errors.New("capture SQL")
		mock.ExpectQuery("total stats").WillReturnRows(sqlmock.NewRows([]string{
			"total_requests", "total_input_tokens", "total_output_tokens", "total_cache_creation_tokens", "total_cache_read_tokens", "total_cost", "total_actual_cost", "total_account_cost", "total_duration_ms",
		}).AddRow(5, 10, 15, 20, 25, 1.0, 0.9, 0.8, 500))
		mock.ExpectQuery("today stats").WithArgs(start).WillReturnError(stop)
		err := repo.fillDashboardUsageStatsAggregated(context.Background(), &DashboardStats{}, start, now)
		require.ErrorIs(t, err, stop)
		require.Len(t, *queries, 2)
		require.Contains(t, (*queries)[1], "total_duration_ms")
	})
	t.Run("usage logs", func(t *testing.T) {
		repo, mock, queries := newDashboardSQLRecorder(t)
		stop := errors.New("capture SQL")
		mock.ExpectQuery("today stats").WithArgs(start.Add(-24*time.Hour), now, start, start.Add(24*time.Hour)).WillReturnError(stop)
		err := repo.fillDashboardUsageStatsFromUsageLogs(context.Background(), &DashboardStats{}, start.Add(-24*time.Hour), now, start, now)
		require.ErrorIs(t, err, stop)
		require.Len(t, *queries, 1)
		require.Contains(t, (*queries)[0], "SUM(duration_ms) FILTER (WHERE created_at >= $3::timestamptz AND created_at < $4::timestamptz)")
	})
}
