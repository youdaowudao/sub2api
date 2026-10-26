package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

func TestGetCacheHitRateStats(t *testing.T) {
	for _, dim := range []string{"total", "group", "account"} {
		t.Run(dim, func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(func(expected, actual string) error {
				// Preserve the real statement for local PostgreSQL EXPLAIN verification.
				encoded, err := json.Marshal(map[string]string{"dimension": dim, "sql": actual})
				if err != nil {
					return err
				}
				t.Logf("CACHE_RATE_SQL %s", encoded)
				return sqlmock.QueryMatcherRegexp.Match(expected, actual)
			})))
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			repo := &usageLogRepository{sql: db}
			start := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
			end := start.AddDate(0, 0, 2)
			filters := usagestats.UsageLogFilters{UserID: 42, GroupID: 3, StartTime: &start, EndTime: &end}
			pattern := `(?s)FROM usage_logs.*WHERE user_id = \$1 AND group_id = \$2 AND created_at >= \$3 AND created_at < \$4`
			if dim == "group" {
				pattern += `.*LEFT JOIN groups`
			}
			if dim == "account" {
				pattern += `.*account_id = ANY\(\$5\).*LEFT JOIN accounts.*ORDER BY .*requests DESC, .*id ASC`
			}
			expect := mock.ExpectQuery(pattern)
			ids := []int64(nil)
			if dim == "account" {
				ids = []int64{1, 3}
				expect.WithArgs(int64(42), int64(3), start, end, sqlmock.AnyArg())
			} else {
				expect.WithArgs(int64(42), int64(3), start, end)
			}
			expect.WillReturnRows(sqlmock.NewRows([]string{"id", "name", "platform", "requests", "input_tokens", "cache_creation_tokens", "cache_read_tokens"}).AddRow(1, "fixture", "anthropic", 2, 10, 10, 80))
			items, err := repo.GetCacheHitRateStats(context.Background(), filters, dim, ids)
			require.NoError(t, err)
			require.Len(t, items, 1)
			require.InDelta(t, 80, *items[0].CacheHitRate, 1e-10)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestGetCacheHitRateStatsEmptyTotal(t *testing.T) {
	db, mock := newSQLMock(t)
	repo := &usageLogRepository{sql: db}
	mock.ExpectQuery(`(?s)FROM usage_logs`).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "platform", "requests", "input_tokens", "cache_creation_tokens", "cache_read_tokens"}))
	items, err := repo.GetCacheHitRateStats(context.Background(), usagestats.UsageLogFilters{}, "total", nil)
	require.NoError(t, err)
	require.Equal(t, []usagestats.CacheHitRateItem{{}}, items)
}

func TestCacheHitRateSharedFilterSQL(t *testing.T) {
	for _, dimension := range []string{"stats", "group-without-group-filter"} {
		t.Run(dimension, func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(func(expected, actual string) error {
				encoded, err := json.Marshal(map[string]string{"dimension": dimension, "sql": actual})
				if err != nil {
					return err
				}
				t.Logf("CACHE_RATE_SQL %s", encoded)
				return sqlmock.QueryMatcherRegexp.Match(expected, actual)
			})))
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			repo := &usageLogRepository{sql: db}
			start := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
			end := start.AddDate(0, 0, 2)
			filters := usagestats.UsageLogFilters{UserID: 42, StartTime: &start, EndTime: &end}
			if dimension == "stats" {
				filters.GroupID = 3
				mock.ExpectQuery(`(?s)FROM usage_logs.*user_id = \$1.*group_id = \$2.*created_at >= \$3.*created_at < \$4`).
					WithArgs(int64(42), int64(3), start, end).WillReturnRows(sqlmock.NewRows([]string{"empty"}))
				_, err = repo.GetStatsWithFilters(context.Background(), filters)
			} else {
				mock.ExpectQuery(`(?s)FROM usage_logs.*user_id = \$1.*created_at >= \$2.*created_at < \$3`).
					WithArgs(int64(42), start, end).WillReturnRows(sqlmock.NewRows([]string{"empty"}))
				_, err = repo.GetCacheHitRateStats(context.Background(), filters, "group", nil)
			}
			require.NoError(t, err)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
