//go:build integration

package repository

import (
	"context"
	"sort"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestVideoRelayRepoAdminStateAndRiskProjection(t *testing.T) {
	ctx := context.Background()
	repo := NewVideoRelayRepository(integrationDB)
	task := newVideoRelayTask(t, 50, 5)
	_, err := integrationDB.ExecContext(ctx, "UPDATE users SET username='fixture user' WHERE id=$1", task.UserID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, "UPDATE accounts SET name='fixture account' WHERE id=$1", task.AccountID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, "UPDATE groups SET name=$2 WHERE id=$1", task.GroupID, task.TaskID)
	require.NoError(t, err)
	type stateFixture struct {
		billing, status, display, reason, upstream, safeID string
		sent, risk                                         bool
	}
	tests := []stateFixture{
		{billing: "held", status: "submitting", display: "processing"},
		{billing: "held", status: "submitting", display: "confirmation", sent: true},
		{billing: "held", status: "unconfirmed", display: "confirmation", sent: true, upstream: "https://example.invalid/test-only"},
		{billing: "held", status: "queued", display: "processing", sent: true, upstream: "safe-id", safeID: "safe-id"},
		{billing: "charged", status: "completed", display: "completed", sent: true, upstream: "complete-42", safeID: "complete-42"},
		{billing: "refunded", status: "failed", display: "failed", reason: "upstream_rejected", sent: true, risk: true},
		{billing: "refunded", status: "failed", display: "failed", reason: "upstream_failed", sent: true},
		{billing: "refunded", status: "failed", display: "failed", reason: "tracking_expired", sent: true, risk: true},
		{billing: "refunded", status: "failed", display: "failed", reason: "not_sent"},
	}
	ids := make(map[string]stateFixture)
	for _, test := range tests {
		item := task
		item.TaskID = videoRelayFixtureID()
		err := repo.Reserve(ctx, &item)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, `
			UPDATE video_relay_tasks SET billing_state=$2,status=$3,failure_reason=$4,upstream_task_id=$5,
				sent_at=CASE WHEN $6 THEN clock_timestamp() END,
				settled_at=CASE WHEN $2 <> 'held' THEN clock_timestamp() END
			WHERE task_id=$1`, item.TaskID, test.billing, test.status, test.reason, test.upstream, test.sent)
		require.NoError(t, err)
		ids[item.TaskID] = test
	}
	// 同一创建时间必须以 task_id DESC 稳定排序，分页前后均保持一致。
	_, err = integrationDB.ExecContext(ctx, "UPDATE video_relay_tasks SET created_at=NOW() WHERE user_id=$1", task.UserID)
	require.NoError(t, err)
	check := func(status string, riskOnly bool, total int64) {
		t.Helper()
		filter := service.VideoRelayAdminFilter{UserID: task.UserID, Status: status, RiskOnly: riskOnly}
		rows, count, err := repo.ListAdmin(ctx, filter, 0, 20)
		require.NoError(t, err)
		require.Equal(t, total, count)
		require.Len(t, rows, int(total))
		for _, row := range rows {
			want, ok := ids[row.TaskID]
			require.True(t, ok)
			require.Equal(t, want.display, row.Status)
			require.Equal(t, want.billing, row.BillingState)
			require.Equal(t, want.reason, row.FailureReason)
			require.Equal(t, want.risk, row.Risk)
			require.Equal(t, want.safeID, row.UpstreamTaskID)
			require.NotEmpty(t, row.UserEmail)
			require.Equal(t, "fixture user", row.UserName)
			require.Equal(t, "fixture account", row.AccountName)
			require.False(t, row.AccountDeleted)
			require.Equal(t, task.TaskID, row.GroupName)
			if status != "" {
				require.Equal(t, status, row.Status)
			}
			if riskOnly {
				require.True(t, row.Risk)
			}
		}
	}
	check("", false, 9)
	check("confirmation", false, 2)
	check("processing", false, 2)
	check("completed", false, 1)
	check("failed", false, 4)
	check("", true, 2)
	check("completed", true, 0)
	page, count, err := repo.ListAdmin(ctx, service.VideoRelayAdminFilter{UserID: task.UserID}, 1, 1)
	require.NoError(t, err)
	require.EqualValues(t, 9, count)
	require.Len(t, page, 1)

	allRows, _, err := repo.ListAdmin(ctx, service.VideoRelayAdminFilter{UserID: task.UserID}, 0, 20)
	require.NoError(t, err)
	require.Equal(t, allRows[1], page[0])
	wantIDs := make([]string, 0, len(ids))
	for id := range ids {
		wantIDs = append(wantIDs, id)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(wantIDs)))
	for i, row := range allRows {
		require.Equal(t, wantIDs[i], row.TaskID)
	}
	_, err = integrationDB.ExecContext(ctx, "UPDATE accounts SET deleted_at=clock_timestamp() WHERE id=$1", task.AccountID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, "UPDATE users SET deleted_at=clock_timestamp() WHERE id=$1", task.UserID)
	require.NoError(t, err)
	rows, count, err := repo.ListAdmin(ctx, service.VideoRelayAdminFilter{TaskID: page[0].TaskID}, 0, 20)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	require.Len(t, rows, 1)
	require.Empty(t, rows[0].UserEmail)
	require.Empty(t, rows[0].UserName)
	require.Equal(t, "fixture account", rows[0].AccountName)
	require.True(t, rows[0].AccountDeleted)
	require.Equal(t, task.UserID, rows[0].UserID)
	require.Equal(t, task.AccountID, rows[0].AccountID)
	_, err = integrationDB.ExecContext(ctx, "UPDATE video_relay_tasks SET account_id=0,group_id=0 WHERE task_id=$1", page[0].TaskID)
	require.NoError(t, err)
	rows, _, err = repo.ListAdmin(ctx, service.VideoRelayAdminFilter{TaskID: page[0].TaskID}, 0, 20)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Empty(t, rows[0].AccountName)
	require.Empty(t, rows[0].GroupName)
	require.False(t, rows[0].AccountDeleted)
	rows, count, err = repo.ListAdmin(ctx, service.VideoRelayAdminFilter{UserID: task.UserID}, 20, 1)
	require.NoError(t, err)
	require.Empty(t, rows)
	require.EqualValues(t, 9, count)
}
