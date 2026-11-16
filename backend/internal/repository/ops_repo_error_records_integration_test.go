//go:build integration

package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newOpsErrorRecordsFixture(t *testing.T) (*opsRepository, *service.OpsErrorRecordsFilter, int64) {
	ctx := context.Background()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{})
	other := mustCreateUser(t, client, &service.User{})
	key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID})
	otherKey := mustCreateApiKey(t, client, &service.APIKey{UserID: other.ID})
	account := mustCreateAccount(t, client, &service.Account{Name: "error-record-regression", Platform: service.PlatformOpenAI})
	start := time.Date(2020, 9, 16, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	ids := []int64{}
	t.Cleanup(func() {
		for _, id := range ids {
			_, _ = integrationDB.ExecContext(ctx, "DELETE FROM ops_error_logs WHERE id=$1", id)
		}
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM api_keys WHERE id IN ($1,$2)", key.ID, otherKey.ID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM accounts WHERE id=$1", account.ID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id IN ($1,$2)", user.ID, other.ID)
	})
	insert := func(n, status int, upstream *int, phase, typ, owner, message string, uid, kid int64, limited bool) {
		t.Helper()
		var aid *int64
		if owner == "provider" {
			aid = &account.ID
		}
		var id int64
		err := integrationDB.QueryRowContext(ctx, `INSERT INTO ops_error_logs
   (created_at,user_id,api_key_id,account_id,platform,model,requested_model,upstream_model,error_phase,error_type,error_owner,error_source,severity,status_code,upstream_status_code,error_message,is_business_limited)
   VALUES($1,$2,$3,$4,'openai','model-a','model-a','model-a',$5,$6,$7,'gateway','P1',$8,$9,$10,$11) RETURNING id`, start.Add(time.Duration(n)*time.Minute), uid, kid, aid, phase, typ, owner, status, upstream, message, limited).Scan(&id)
		require.NoError(t, err)
		ids = append(ids, id)
	}
	up, zero := 503, 0
	insert(1, 502, &up, "upstream", "upstream_error", "provider", "service unavailable request_id=req-one000", user.ID, key.ID, false)
	insert(2, 502, &up, "upstream", "upstream_error", "provider", "service unavailable request_id=req-two000", other.ID, otherKey.ID, false)
	insert(3, 200, &up, "upstream", "upstream_error", "provider", "service unavailable request_id=req-three000", user.ID, key.ID, false)
	insert(4, 403, nil, "request", "billing_error", "client", "Insufficient account balance", user.ID, key.ID, true)
	insert(5, 429, nil, "request", "rate_limit_error", "client", "concurrency limit exceeded", user.ID, key.ID, true)
	insert(6, 200, nil, "request", "cyber_policy", "client", "policy rejected", user.ID, key.ID, true)
	insert(7, 200, &zero, "account_auth", "api_error", "provider", "credential acquisition failed", user.ID, key.ID, false)
	insert(8, 200, nil, "internal", "api_error", "platform", "normal success", user.ID, key.ID, false)
	repo := NewOpsRepository(integrationDB).(*opsRepository)
	filter := &service.OpsErrorRecordsFilter{OpsErrorLogFilter: service.OpsErrorLogFilter{StartTime: &start, EndTime: &end, Page: 1, PageSize: 10, View: "all"}, Scope: "all", Display: "grouped", Finance: "hide"}
	return repo, filter, user.ID
}

func TestOpsErrorRecordsScopeGroupingAndMembers(t *testing.T) {
	ctx := context.Background()
	repo, filter, userID := newOpsErrorRecordsFixture(t)
	result, err := repo.ListErrorRecords(ctx, filter)
	require.NoError(t, err)
	require.EqualValues(t, 6, result.TotalRecords)
	require.EqualValues(t, 5, result.Total)
	var repeated *service.OpsErrorRecord
	for _, row := range result.Items {
		if row.Count == 2 {
			repeated = row
		}
	}
	require.NotNil(t, repeated, "duplicate failures must collapse across users, without merging recovered rows")
	require.EqualValues(t, 2, repeated.UserCount)
	require.EqualValues(t, 2, repeated.APIKeyCount)
	require.Nil(t, repeated.SingleUser)
	require.True(t, repeated.IsRequestError)
	require.True(t, repeated.IsUpstreamError)
	members, err := repo.ListErrorRecordMembers(ctx, filter, repeated.RepresentativeID)
	require.NoError(t, err)
	require.EqualValues(t, 2, members.Total)
	require.Len(t, members.Items, 2)
	filter.Page = 99
	filter.PageSize = 1
	empty, err := repo.ListErrorRecords(ctx, filter)
	require.NoError(t, err)
	require.Empty(t, empty.Items)
	require.EqualValues(t, 5, empty.Total)
	require.EqualValues(t, 6, empty.TotalRecords)
	filter.Page = 1
	filter.PageSize = 10
	filter.Display = "raw"
	filter.Finance = "include"
	raw, err := repo.ListErrorRecords(ctx, filter)
	require.NoError(t, err)
	require.EqualValues(t, 7, raw.Total)
	filter.Scope = "request"
	req, err := repo.ListErrorRecords(ctx, filter)
	require.NoError(t, err)
	require.EqualValues(t, 5, req.Total)
	filter.Scope = "upstream"
	provider, err := repo.ListErrorRecords(ctx, filter)
	require.NoError(t, err)
	require.EqualValues(t, 4, provider.Total)
	filter.StatusCodes = []int{0}
	auth, err := repo.ListErrorRecords(ctx, filter)
	require.NoError(t, err)
	require.EqualValues(t, 1, auth.Total)
	filter.StatusCodes = nil
	filter.Finance = "only"
	noFinance, err := repo.ListErrorRecords(ctx, filter)
	require.NoError(t, err)
	require.Zero(t, noFinance.Total)
	filter.Scope = "all"
	finance, err := repo.ListErrorRecords(ctx, filter)
	require.NoError(t, err)
	require.EqualValues(t, 1, finance.Total)
	filter.Finance = "hide"
	filter.UserID = &userID
	_, err = repo.ListErrorRecordMembers(ctx, filter, repeated.RepresentativeID)
	require.ErrorIs(t, err, sql.ErrNoRows, "a representative outside current filters must not silently widen members")
}
