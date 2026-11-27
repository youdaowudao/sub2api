//go:build integration

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type accountPerformanceSQLCapture struct {
	sqlExecutor
	queries int
	query   string
	args    []any
}

func (s *accountPerformanceSQLCapture) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	s.queries++
	s.query, s.args = query, args
	return s.sqlExecutor.QueryContext(ctx, query, args...)
}

func TestAccountPerformanceAggregatesActualPostgresRows(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := newUsageLogRepositoryWithSQL(client, tx)
	user := mustCreateUser(t, client, &service.User{Email: "account-perf@test.com"})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID})
	a := mustCreateAccount(t, client, &service.Account{Name: "perf-success", Platform: service.PlatformOpenAI})
	b := mustCreateAccount(t, client, &service.Account{Name: "perf-error-only", Platform: service.PlatformOpenAI})
	c := mustCreateAccount(t, client, &service.Account{Name: "perf-idle", Platform: service.PlatformOpenAI})
	start := time.Now().UTC().Truncate(time.Minute).Add(-time.Hour)
	end := start.Add(time.Hour)
	at := start.Add(time.Minute)
	dur := 1200
	for _, when := range []time.Time{start, at, end} {
		_, err := repo.Create(ctx, &service.UsageLog{UserID: user.ID, APIKeyID: key.ID, AccountID: a.ID, Model: "perf-model", InputTokens: 20, CacheReadTokens: 80, DurationMs: &dur, CreatedAt: when})
		require.NoError(t, err)
	}
	insert := func(id int64, owner string, status, upstream int, when time.Time) {
		t.Helper()
		_, err := tx.ExecContext(ctx, `INSERT INTO ops_error_logs
		(created_at,user_id,api_key_id,account_id,platform,model,requested_model,error_phase,error_type,error_owner,error_source,severity,status_code,upstream_status_code,error_message)
		VALUES($1,$2,$3,$4,'openai','perf-model','perf-model','upstream','upstream_error',$5,'gateway','P1',$6,$7,'fixture')`,
			when, user.ID, key.ID, id, owner, status, upstream)
		require.NoError(t, err)
	}
	insert(a.ID, "provider", 200, 503, at)
	insert(a.ID, "provider", 502, 429, at)
	insert(a.ID, "client", 429, 429, at)
	insert(b.ID, "provider", 502, 503, at)
	insert(b.ID, "provider", 502, 503, end)
	insert(c.ID, "client", 403, 403, at)
	filters := usagestats.UsageLogFilters{UserID: user.ID, Model: "perf-model", ModelFilterSource: usagestats.ModelSourceRequested, StartTime: &start, EndTime: &end}
	capture := &accountPerformanceSQLCapture{sqlExecutor: tx}
	reader := newUsageLogRepositoryWithSQL(client, capture)
	type row struct {
		ID           int64    `json:"id"`
		Requests     int64    `json:"requests"`
		CacheHitRate *float64 `json:"cache_hit_rate"`
		Errors       *int64   `json:"upstream_error_count"`
		Limits       *int64   `json:"rate_limit_count"`
		Average      *float64 `json:"average_duration_ms"`
	}
	// CI's RED runner compiles every changed repository test with the old
	// production package. Resolve the new method here, so this positive new
	// feature test doesn't prevent the separate timer RED targets compiling.
	load := func(ids []int64, errors bool) []row {
		t.Helper()
		method := reflect.ValueOf(reader).MethodByName("GetAccountPerformanceStats")
		require.True(t, method.IsValid(), "account performance reader must exist")
		before := capture.queries
		values := method.Call([]reflect.Value{reflect.ValueOf(ctx), reflect.ValueOf(filters), reflect.ValueOf(ids), reflect.ValueOf("openai"), reflect.ValueOf(errors)})
		require.True(t, values[1].IsNil(), "%v", values[1].Interface())
		require.Equal(t, before+1, capture.queries, "each account summary must use one SQL read")
		raw, err := json.Marshal(values[0].Interface())
		require.NoError(t, err)
		var result []row
		require.NoError(t, json.Unmarshal(raw, &result))
		return result
	}
	got := load(nil, true)
	require.Len(t, got, 2)
	require.Equal(t, a.ID, got[0].ID)
	require.EqualValues(t, 2, got[0].Requests)
	require.NotNil(t, got[0].CacheHitRate)
	require.NotNil(t, got[0].Average)
	require.NotNil(t, got[0].Errors)
	require.NotNil(t, got[0].Limits)
	require.EqualValues(t, 80, *got[0].CacheHitRate)
	require.EqualValues(t, 1200, *got[0].Average)
	require.EqualValues(t, 2, *got[0].Errors)
	require.EqualValues(t, 1, *got[0].Limits)
	require.Equal(t, b.ID, got[1].ID)
	require.Zero(t, got[1].Requests)
	require.Nil(t, got[1].CacheHitRate)
	require.Nil(t, got[1].Average)
	require.NotNil(t, got[1].Errors)
	require.EqualValues(t, 1, *got[1].Errors)
	// A direct count using the same raw detail predicate must agree.
	f := &service.OpsErrorRecordsFilter{Scope: "upstream", Finance: "include", Display: "raw", OpsErrorLogFilter: service.OpsErrorLogFilter{UserID: &user.ID, AccountID: &a.ID, StartTime: &start, EndTime: &end, Model: "perf-model", View: "all", Platform: "openai"}}
	where, args := buildOpsErrorRecordsWhere(f)
	detailRows, err := tx.QueryContext(ctx, "SELECT COUNT(*) FROM ops_error_logs e "+where, args...)
	require.NoError(t, err)
	defer func() { _ = detailRows.Close() }()
	require.True(t, detailRows.Next())
	var count int64
	require.NoError(t, detailRows.Scan(&count))
	require.Equal(t, count, *got[0].Errors)
	require.NoError(t, detailRows.Close())
	require.Len(t, load([]int64{b.ID}, true), 1)
	disabled := load(nil, false)
	require.Len(t, disabled, 1)
	require.Nil(t, disabled[0].Errors)
	require.Nil(t, disabled[0].Limits)
	// Real isolated PostgreSQL query plans, not estimates of production latency.
	for _, window := range []struct {
		name string
		from time.Time
	}{{"30m", end.Add(-30 * time.Minute)}, {"1h", end.Add(-time.Hour)}, {"3h", end.Add(-3 * time.Hour)}, {"today", end.Truncate(24 * time.Hour)}} {
		filters.StartTime = &window.from
		started := time.Now()
		_ = load(nil, true)
		elapsed := time.Since(started)
		plans, err := tx.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+capture.query, capture.args...)
		require.NoError(t, err)
		require.True(t, plans.Next())
		var raw []byte
		require.NoError(t, plans.Scan(&raw))
		require.NoError(t, plans.Close())
		t.Logf("account performance fixture %s: one read, elapsed=%s; plan=%s", window.name, elapsed, raw)
	}
}
