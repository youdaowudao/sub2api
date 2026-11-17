//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Explicitly opt in to a loopback browser fixture. Ordinary tests never open it.
func TestOpsErrorRecordsBrowserFixture(t *testing.T) {
	if os.Getenv("OPS_ERRORS_BROWSER_FIXTURE") != "1" {
		t.Skip("loopback browser fixture is opt-in")
	}
	repo, filter, userID := newOpsErrorRecordsFixture(t)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM ops_error_logs WHERE request_id LIKE 'ops-browser-extra-%'")
	})
	svc := service.NewOpsService(repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler := admin.NewOpsHandler(svc)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/v1/admin/ops/error-records", handler.ListErrorRecords)
	router.GET("/api/v1/admin/ops/error-records/:representative_id/members", handler.ListErrorRecordMembers)
	router.GET("/api/v1/admin/ops/request-errors/:id", handler.GetRequestError)
	router.GET("/api/v1/admin/ops/request-errors/:id/upstream-errors", handler.ListRequestErrorUpstreamErrors)
	router.GET("/api/v1/admin/ops/upstream-errors/:id", handler.GetUpstreamError)
	server := httptest.NewServer(router)
	defer server.Close()
	root, err := filepath.Abs("../../../.local/ops-errors")
	require.NoError(t, err)
	done := filepath.Join(root, fmt.Sprintf("browser-finished-%d", os.Getpid()))
	control := map[string]any{"api_url": server.URL, "window_start": filter.StartTime, "window_end": filter.EndTime, "user_id": userID, "pid": os.Getpid(), "done_file": done}
	encoded, err := json.MarshalIndent(control, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(root, "browser-control.json"), encoded, 0600))
	t.Log("Loopback error-record API fixture is ready with isolated PostgreSQL")
	deadline := time.NewTimer(20 * time.Minute)
	defer deadline.Stop()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("browser fixture timed out")
		case <-tick.C:
			if _, err := os.Stat(done); err == nil {
				return
			}
		}
	}
}

// Latency thresholds are measured only on the explicitly selected local machine.
// Functional SQL assertions always run in the ordinary integration regressions.
func TestOpsErrorRecordsPerformance(t *testing.T) {
	if os.Getenv("OPS_ERRORS_PERF") != "1" {
		t.Skip("local latency sampling is opt-in")
	}
	ctx := context.Background()
	start := time.Date(2017, 11, 16, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM ops_error_logs WHERE created_at >= $1 AND created_at < $2", start.Add(-48*time.Hour), end)
	})
	repo := NewOpsRepository(integrationDB).(*opsRepository)
	filter := &service.OpsErrorRecordsFilter{OpsErrorLogFilter: service.OpsErrorLogFilter{StartTime: &start, EndTime: &end, Page: 1, PageSize: 10, View: "all"}, Scope: "all", Finance: "hide", Display: "grouped"}
	dashboard := &service.OpsDashboardFilter{StartTime: start, EndTime: end, QueryMode: service.OpsQueryModeRaw}
	report := map[string]any{}
	t.Cleanup(func() {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err == nil {
			_ = os.WriteFile(filepath.Join("..", "..", "..", ".local", "ops-errors", "evidence", "sql-performance.json"), encoded, 0600)
		}
	})
	p95 := func(samples []float64) float64 {
		copy := append([]float64(nil), samples...)
		sort.Float64s(copy)
		return copy[len(copy)-1]
	}
	for _, density := range []string{"high", "low"} {
		_, err := integrationDB.ExecContext(ctx, "DELETE FROM ops_error_logs WHERE created_at >= $1 AND created_at < $2", start.Add(-48*time.Hour), end)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, `INSERT INTO ops_error_logs(created_at,error_phase,error_type,error_owner,error_source,severity,status_code,upstream_status_code,error_message,account_id,user_id,model)
  SELECT $1::timestamptz+(n%3600)*interval '1 second'-CASE WHEN n%3=0 THEN interval '48 hours' ELSE interval '0' END,
  'upstream','upstream_error','provider','upstream_http','P1',502,503,
  CASE WHEN $2='high' THEN 'service unavailable request_id=benchmark-'||n ELSE 'different cause '||n END,
  100000000+(n%5),100000000+(n%200),'model-a' FROM generate_series(1,100000) n`, start, density)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, "ANALYZE ops_error_logs")
		require.NoError(t, err)
		query, args, _, _ := buildOpsErrorRecordsQuery(filter, 0)
		diagnostic, err := json.Marshal(map[string]any{"sql": query, "args": args})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join("..", "..", "..", ".local", "ops-errors", "evidence", "sql-query.json"), diagnostic, 0600))
		var plan []byte
		err = integrationDB.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&plan)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join("..", "..", "..", ".local", "ops-errors", "evidence", "sql-explain-"+density+".json"), plan, 0600))
		latency := []float64{}
		for i := 0; i < 10; i++ {
			queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			at := time.Now()
			result, err := repo.ListErrorRecords(queryCtx, filter)
			latency = append(latency, float64(time.Since(at).Microseconds())/1000)
			cancel()
			require.NoError(t, err)
			require.EqualValues(t, 66667, result.TotalRecords)
		}
		baseline, candidate := []float64{}, []float64{}
		for i := 0; i < 10; i++ {
			at := time.Now()
			_, err := repo.GetDashboardOverview(ctx, dashboard)
			baseline = append(baseline, float64(time.Since(at).Microseconds())/1000)
			require.NoError(t, err)
			finished := make(chan error, 1)
			go func() {
				queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				_, err := repo.ListErrorRecords(queryCtx, filter)
				finished <- err
			}()
			at = time.Now()
			_, err = repo.GetDashboardOverview(ctx, dashboard)
			candidate = append(candidate, float64(time.Since(at).Microseconds())/1000)
			require.NoError(t, err)
			require.NoError(t, <-finished)
		}
		report[density] = map[string]any{"list_ms": latency, "list_p95_ms": p95(latency), "overview_baseline_ms": baseline, "overview_with_list_ms": candidate, "overview_baseline_p95_ms": p95(baseline), "overview_with_list_p95_ms": p95(candidate)}
		require.LessOrEqual(t, p95(latency), 2000.0, "default 1h grouped query budget")
		require.LessOrEqual(t, p95(candidate)-p95(baseline), max(100.0, p95(baseline)*0.1), "overview must retain its latency budget")
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	require.NoError(t, err)
	path := filepath.Join("..", "..", "..", ".local", "ops-errors", "evidence", "sql-performance.json")
	require.NoError(t, os.WriteFile(path, encoded, 0600))
}
