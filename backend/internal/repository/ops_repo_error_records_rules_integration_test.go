//go:build integration

package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpsErrorRecordsFinanceAndConservativeGrouping(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2019, 8, 17, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM ops_error_logs WHERE created_at >= $1 AND created_at < $2", start, end)
	})
	insert := func(owner, phase, message string, account, user any, model string) {
		t.Helper()
		_, err := integrationDB.ExecContext(ctx, `INSERT INTO ops_error_logs(created_at,error_phase,error_type,error_owner,error_source,severity,status_code,error_message,account_id,user_id,model)
  VALUES($1,$2,'api_error',$3,'gateway','P1',503,$4,$5,$6,$7)`, start, phase, owner, message, account, user, model)
		require.NoError(t, err)
	}
	phrases := []string{"insufficient balance", "insufficient account balance", "账户余额不足", "api key 额度已用完", "api key 5小时限额已用完", "api key 日限额已用完", "api key 7天限额已用完", "daily usage limit exceeded", "weekly usage limit exceeded", "monthly usage limit exceeded", "usage quota exhausted for this platform"}
	for _, phrase := range phrases {
		insert("client", "request", phrase, nil, int64(1), "a")
		insert("provider", "upstream", phrase, int64(1), int64(1), "a")
	}
	for _, message := range []string{"subscription is invalid or expired", "requests-per-minute limit exceeded", "concurrency limit exceeded", "no available accounts", "billing service temporarily unavailable"} {
		insert("client", "request", message, nil, int64(1), "a")
	}
	insert("", "request", "insufficient balance", nil, int64(1), "a")
	repo := NewOpsRepository(integrationDB).(*opsRepository)
	f := &service.OpsErrorRecordsFilter{OpsErrorLogFilter: service.OpsErrorLogFilter{StartTime: &start, EndTime: &end, Page: 1, PageSize: 100, View: "all"}, Scope: "all", Finance: "only", Display: "raw"}
	only, err := repo.ListErrorRecords(ctx, f)
	require.NoError(t, err)
	require.EqualValues(t, len(phrases), only.Total)
	f.Finance = "hide"
	hidden, err := repo.ListErrorRecords(ctx, f)
	require.NoError(t, err)
	require.EqualValues(t, len(phrases)+6, hidden.Total, "provider balance and non-financial local errors remain visible")
	_, err = integrationDB.ExecContext(ctx, "DELETE FROM ops_error_logs WHERE created_at >= $1 AND created_at < $2", start, end)
	require.NoError(t, err)
	// Equal transport metadata collapses. Different causes and unknown ownership do not.
	for _, message := range []string{`timeout request_id=req-one123 timestamp=2026-11-16T01:02:03Z`, `timeout request_id=req-two123 timestamp=2026-11-16T01:03:04Z`} {
		insert("provider", "upstream", message, int64(1), int64(1), "a")
	}
	insert("provider", "upstream", "limit 1024", int64(1), int64(1), "a")
	insert("provider", "upstream", "limit 2048", int64(1), int64(1), "a")
	insert("provider", "upstream", "limit 1024", int64(2), int64(1), "a")
	insert("provider", "upstream", "limit 1024", int64(1), int64(1), "b")
	for i := 0; i < 2; i++ {
		insert("provider", "upstream", "unknown account", nil, int64(1), "a")
		insert("provider", "upstream", "", int64(1), int64(1), "a")
	}
	insert("client", "request", "local refusal", nil, int64(1), "a")
	insert("client", "request", "local refusal", nil, int64(2), "a")
	f.Display = "grouped"
	f.Finance = "include"
	grouped, err := repo.ListErrorRecords(ctx, f)
	require.NoError(t, err)
	require.EqualValues(t, 12, grouped.TotalRecords)
	require.EqualValues(t, 11, grouped.Total)
	repeated := 0
	for _, row := range grouped.Items {
		if row.Count > 1 {
			repeated++
			require.EqualValues(t, 2, row.Count)
			require.Contains(t, row.Representative.Message, "timeout")
		}
	}
	require.Equal(t, 1, repeated)
}

func TestOpsErrorRecordsGroupingBoundariesAndSort(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2018, 6, 17, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM ops_error_logs WHERE created_at >= $1 AND created_at < $2", start, end)
	})
	repo := NewOpsRepository(integrationDB).(*opsRepository)
	f := &service.OpsErrorRecordsFilter{OpsErrorLogFilter: service.OpsErrorLogFilter{StartTime: &start, EndTime: &end, Page: 1, PageSize: 100, View: "all"}, Scope: "all", Finance: "include", Display: "grouped"}
	for _, change := range []string{"platform='anthropic'", "group_id=2", "account_id=2", "requested_model='b'", "upstream_model='b'", "inbound_endpoint='/responses'", "upstream_endpoint='/responses'", "request_type=2", "stream=true", "error_phase='account_auth'", "error_type='other'", "error_owner='platform'", "error_source='other'", "severity='P2'", "upstream_status_code=504", "status_code=200", "error_message='limit 2048'", "inbound_endpoint=''"} {
		t.Run(change, func(t *testing.T) {
			_, err := integrationDB.ExecContext(ctx, "DELETE FROM ops_error_logs WHERE created_at >= $1 AND created_at < $2", start, end)
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(ctx, `INSERT INTO ops_error_logs(created_at,error_phase,error_type,error_owner,error_source,severity,status_code,upstream_status_code,error_message,account_id,user_id,api_key_id,platform,model,requested_model,upstream_model,inbound_endpoint,upstream_endpoint,request_type,stream)
 SELECT $1::timestamptz+n*interval '1 minute','upstream','upstream_error','provider','upstream_http','P1',502,503,'limit 1024',1,n,n,'openai','a','a','a','/chat','/chat',1,false FROM generate_series(1,2)n`, start)
			require.NoError(t, err)
			r, err := repo.ListErrorRecords(ctx, f)
			require.NoError(t, err)
			require.EqualValues(t, 1, r.Total)
			_, err = integrationDB.ExecContext(ctx, "UPDATE ops_error_logs SET "+change+" WHERE created_at=$1", start.Add(2*time.Minute))
			require.NoError(t, err)
			r, err = repo.ListErrorRecords(ctx, f)
			require.NoError(t, err)
			require.EqualValues(t, 2, r.Total, "a changed cause/context must not collapse")
			for _, sortBy := range []string{"created_at", "model", "status_code"} {
				f.SortBy = sortBy
				f.SortOrder = "asc"
				ascending, e := repo.ListErrorRecords(ctx, f)
				require.NoError(t, e)
				f.SortOrder = "desc"
				descending, e := repo.ListErrorRecords(ctx, f)
				require.NoError(t, e)
				require.Equal(t, ascending.Items[0].RepresentativeID, descending.Items[1].RepresentativeID)
			}
		})
	}
}

func TestOpsErrorRecordsNormalizationDoesNotMergeUnsupportedValues(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2016, 5, 17, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM ops_error_logs WHERE created_at >= $1 AND created_at < $2", start, end)
	})
	repo := NewOpsRepository(integrationDB).(*opsRepository)
	f := &service.OpsErrorRecordsFilter{OpsErrorLogFilter: service.OpsErrorLogFilter{StartTime: &start, EndTime: &end, Page: 1, PageSize: 10, View: "all"}, Scope: "all", Finance: "include", Display: "grouped"}
	for _, tt := range []struct {
		name, a, b string
		same       bool
	}{
		{"short", "request_id=abc", "request_id=def", false},
		{"long", "request_id=" + strings.Repeat("a", 129), "request_id=" + strings.Repeat("b", 129), false},
		{"invalid suffix", "request_id=abcd.foo", "request_id=efgh.foo", false},
		{"literal ID marker", "request_id=<id>", "request_id=valid-123", false},
		{"literal time marker", "timestamp=<time>", "timestamp=2026-11-16T01:02:03Z", false},
		{"valid minimum", "request_id=abcd", "request_id=efgh", true},
		{"valid maximum", "request_id=" + strings.Repeat("a", 128), "request_id=" + strings.Repeat("b", 128), true},
		{"quoted client", "\"client-request-id\": \"abcd\"", "\"client-request-id\": \"efgh\"", true},
		{"trace", "trace-id=abcd", "trace-id=efgh", true},
		{"uppercase", "REQUEST_ID=abcd", "REQUEST_ID=efgh", true},
		{"duplicate fields", "request_id=abcd request_id=efgh", "request_id=ijkl request_id=mnop", true},
		{"embedded name", "notrequest_id=abcd", "notrequest_id=efgh", false},
		{"leading ID hyphen", "other-request_id=abcd", "other-request_id=efgh", false},
		{"trim and escape", "  <literal> request_id=abcd", "  <literal> request_id=efgh", true},
		{"unicode prefix", "İ failure request_id=abcd", "İ failure request_id=efgh", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := integrationDB.ExecContext(ctx, "DELETE FROM ops_error_logs WHERE created_at >= $1 AND created_at < $2", start, end)
			require.NoError(t, err)
			for _, msg := range []string{tt.a, tt.b} {
				_, err = integrationDB.ExecContext(ctx, `INSERT INTO ops_error_logs(created_at,error_phase,error_type,error_owner,error_source,severity,status_code,error_message,account_id) VALUES($1,'upstream','upstream_error','provider','gateway','P1',503,$2,1)`, start, msg)
				require.NoError(t, err)
			}
			rows, err := repo.ListErrorRecords(ctx, f)
			require.NoError(t, err)
			want := int64(2)
			if tt.same {
				want = 1
			}
			require.Equal(t, want, rows.Total)
		})
	}
}

func TestOpsErrorRecordsModelSortMatchesDisplayedModel(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2014, 5, 17, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM ops_error_logs WHERE created_at >= $1 AND created_at < $2", start, end)
	})
	var ids []int64
	// The primary visible label is requested model when present, otherwise
	// upstream model, then the trimmed legacy model.
	for _, models := range [][3]string{{"", "a-upstream", "z-base"}, {" ", " ", " m-model "}, {"z-request", "a-upstream", "base-z"}} {
		var id int64
		err := integrationDB.QueryRowContext(ctx, `INSERT INTO ops_error_logs(created_at,error_phase,error_type,error_owner,status_code,error_message,account_id,requested_model,upstream_model,model)
 VALUES($1,'upstream','upstream_error','provider',503,'unavailable',1,$2,$3,$4) RETURNING id`, start, models[0], models[1], models[2]).Scan(&id)
		require.NoError(t, err)
		ids = append(ids, id)
	}
	repo := NewOpsRepository(integrationDB).(*opsRepository)
	for _, display := range []string{"grouped", "raw"} {
		for _, direction := range []string{"asc", "desc"} {
			t.Run(display+"/"+direction, func(t *testing.T) {
				f := &service.OpsErrorRecordsFilter{OpsErrorLogFilter: service.OpsErrorLogFilter{StartTime: &start, EndTime: &end, Page: 1, PageSize: 10, View: "all", SortBy: "model", SortOrder: direction}, Scope: "all", Finance: "include", Display: display}
				result, err := repo.ListErrorRecords(ctx, f)
				require.NoError(t, err)
				var actual []int64
				for _, item := range result.Items {
					actual = append(actual, item.RepresentativeID)
				}
				expected := append([]int64(nil), ids...)
				if direction == "desc" {
					expected[0], expected[2] = expected[2], expected[0]
				}
				require.Equal(t, expected, actual)
			})
		}
	}
}

func TestOpsErrorRecordsGroupedModelSortUsesRepresentative(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2013, 5, 17, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM ops_error_logs WHERE created_at >= $1 AND created_at < $2", start, end)
	})
	var ids []int64
	for i, models := range [][3]string{{"z", "a", "z"}, {"", "a", "z"}, {"b", "b", "b"}} {
		var id int64
		err := integrationDB.QueryRowContext(ctx, `INSERT INTO ops_error_logs(created_at,error_phase,error_type,error_owner,status_code,error_message,account_id,requested_model,upstream_model,model)
 VALUES($1,'upstream','upstream_error','provider',503,'unavailable',1,$2,$3,$4) RETURNING id`, start.Add(time.Duration(i)*time.Minute), models[0], models[1], models[2]).Scan(&id)
		require.NoError(t, err)
		ids = append(ids, id)
	}
	repo := NewOpsRepository(integrationDB).(*opsRepository)
	for _, direction := range []string{"asc", "desc"} {
		t.Run(direction, func(t *testing.T) {
			f := &service.OpsErrorRecordsFilter{OpsErrorLogFilter: service.OpsErrorLogFilter{StartTime: &start, EndTime: &end, Page: 1, PageSize: 10, View: "all", SortBy: "model", SortOrder: direction}, Scope: "all", Finance: "include", Display: "grouped"}
			result, err := repo.ListErrorRecords(ctx, f)
			require.NoError(t, err)
			require.EqualValues(t, 2, result.Total)
			require.EqualValues(t, 3, result.TotalRecords)
			expected := []int64{ids[1], ids[2]}
			if direction == "desc" {
				expected[0], expected[1] = expected[1], expected[0]
			}
			require.Equal(t, expected, []int64{result.Items[0].RepresentativeID, result.Items[1].RepresentativeID})
		})
	}
}
