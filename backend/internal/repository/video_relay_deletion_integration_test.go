//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestVideoRelayRepoOriginalSourceDeletionSettlement(t *testing.T) {
	for _, test := range []struct {
		table, reason string
	}{
		{"users", service.VideoRelayUserDeleted},
		{"api_keys", service.VideoRelayAPIKeyDeleted},
		{"accounts", service.VideoRelayAccountDeleted},
	} {
		t.Run(test.table, func(t *testing.T) {
			ctx := context.Background()
			task := newVideoRelayTask(t, 10, 5)
			repo := NewVideoRelayRepository(integrationDB)
			require.NoError(t, repo.Reserve(ctx, &task))
			sent, err := repo.AcquireSend(ctx, &task)
			require.NoError(t, err)
			sent, err = repo.RecordSubmission(ctx, sent, "fixture-request", "fixture-provider")
			require.NoError(t, err)
			sourceID := map[string]int64{
				"users": task.UserID, "api_keys": task.APIKeyID, "accounts": task.AccountID,
			}[test.table]
			connection := videoRelayIndependentDB(t)
			_, err = connection.ExecContext(ctx, "UPDATE "+test.table+" SET deleted_at=clock_timestamp() WHERE id=$1", sourceID)
			require.NoError(t, err)
			refunded, result, err := repo.Settle(ctx, sent, service.VideoRelaySettlement{Completed: true})
			require.NoError(t, err)
			require.True(t, result.Applied)
			require.Equal(t, test.reason, refunded.FailureReason)
			require.Equal(t, service.VideoRelayRefunded, refunded.BillingState)
			require.False(t, refunded.CanDownload())
			again, result, err := repo.Settle(ctx, sent, service.VideoRelaySettlement{Completed: true})
			require.NoError(t, err)
			require.Nil(t, result)
			require.Equal(t, refunded.SettledAt, again.SettledAt)
			assertVideoRelayFunds(t, connection, task, 10, 0, 0)
		})
	}
}

func TestVideoRelayRepoDeletionLocksAvoidWaitingAndPermitForeignKeys(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	task := newVideoRelayTask(t, 10, 5)
	connection := videoRelayIndependentDB(t)
	repo := NewVideoRelayRepository(integrationDB)
	require.NoError(t, repo.Reserve(ctx, &task))
	sent, err := repo.AcquireSend(ctx, &task)
	require.NoError(t, err)
	accepted, err := repo.RecordSubmission(ctx, sent, "locked-source", "known-video")
	require.NoError(t, err)
	tx, err := connection.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, "SELECT id FROM api_keys WHERE id=$1 FOR NO KEY UPDATE", task.APIKeyID)
	require.NoError(t, err)
	_, applied, err := repo.Settle(ctx, accepted, service.VideoRelaySettlement{Completed: true})
	var postgresError *pq.Error
	require.ErrorAs(t, err, &postgresError)
	require.Equal(t, pq.ErrorCode("55P03"), postgresError.Code, "the contender must back off the reverse lock order")
	require.Nil(t, applied)
	assertVideoRelayFunds(t, connection, task, 5, 5, 0)
	require.NoError(t, tx.Rollback())

	foreignKey, err := connection.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer foreignKey.Rollback()
	for table, id := range map[string]int64{"users": task.UserID, "api_keys": task.APIKeyID, "accounts": task.AccountID} {
		_, err = foreignKey.ExecContext(ctx, "SELECT id FROM "+table+" WHERE id=$1 FOR KEY SHARE", id)
		require.NoError(t, err)
	}
	settled, _, err := repo.Settle(ctx, accepted, service.VideoRelaySettlement{Completed: true})
	require.NoError(t, err)
	require.Equal(t, service.VideoRelayCharged, settled.BillingState)
	require.NoError(t, foreignKey.Commit())
	_, err = connection.ExecContext(ctx, "UPDATE api_keys SET deleted_at=clock_timestamp() WHERE id=$1", task.APIKeyID)
	require.NoError(t, err)
	afterDelete, result, err := repo.Settle(ctx, accepted, service.VideoRelaySettlement{Completed: true})
	require.NoError(t, err)
	require.Nil(t, result)
	require.Equal(t, service.VideoRelayCharged, afterDelete.BillingState)
	assertVideoRelayFunds(t, connection, task, 5, 0, 1)
}

func TestVideoRelayRepoDeletionGraceAndLateSendEvidence(t *testing.T) {
	for _, test := range []struct {
		name, reason string
	}{
		{"credible provider ID remains refunded", ""},
		{"proved unsent clears pause", service.VideoRelayNotSent},
		{"late definite rejection clears pause", service.VideoRelayUpstreamRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			task := newVideoRelayTask(t, 10, 5)
			repo := NewVideoRelayRepository(integrationDB)
			require.NoError(t, repo.Reserve(ctx, &task))
			sent, err := repo.AcquireSend(ctx, &task)
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(ctx, "UPDATE api_keys SET deleted_at=clock_timestamp() WHERE id=$1", task.APIKeyID)
			require.NoError(t, err)
			makeVideoRelayTaskDue(t, integrationDB, task.TaskID)
			claim, err := repo.ClaimDue(ctx)
			require.NoError(t, err)
			require.NotNil(t, claim)
			require.Equal(t, task.TaskID, claim.TaskID)
			held, result, err := repo.Settle(ctx, claim, service.VideoRelaySettlement{FailureReason: service.VideoRelayAPIKeyDeleted})
			require.NoError(t, err)
			require.Nil(t, result)
			require.Equal(t, service.VideoRelayHeld, held.BillingState)
			assertVideoRelayFunds(t, integrationDB, task, 5, 5, 0)

			_, err = integrationDB.ExecContext(ctx, "UPDATE video_relay_tasks SET sent_at=clock_timestamp()-interval '4 minutes' WHERE task_id=$1", task.TaskID)
			require.NoError(t, err)
			sent, err = repo.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
			require.NoError(t, err)
			refunded, result, err := repo.Settle(ctx, claim, service.VideoRelaySettlement{FailureReason: service.VideoRelayAPIKeyDeleted})
			require.NoError(t, err)
			require.True(t, result.Applied)
			require.Equal(t, service.VideoRelayAPIKeyDeleted, refunded.FailureReason)
			finished := refunded
			switch test.reason {
			case "":
				finished, err = repo.RecordSubmission(ctx, sent, "fixture-late-request", "fixture-late-provider")
				require.NoError(t, err)
				require.Equal(t, "fixture-late-provider", finished.UpstreamTaskID)
				require.Equal(t, service.VideoRelayAPIKeyDeleted, finished.FailureReason)
			case service.VideoRelayNotSent:
				finished, result, err = repo.Settle(ctx, sent, service.VideoRelaySettlement{FailureReason: test.reason})
				require.NoError(t, err)
				require.Nil(t, result)
				require.Equal(t, test.reason, finished.FailureReason)
			case service.VideoRelayUpstreamRejected:
				sent, err = repo.RecordSubmission(ctx, sent, "fixture-late-rejection", "")
				require.NoError(t, err)
				finished, result, err = repo.Settle(ctx, sent, service.VideoRelaySettlement{FailureReason: test.reason})
				require.NoError(t, err)
				require.Nil(t, result)
				require.Equal(t, test.reason, finished.FailureReason)
			}
			require.Equal(t, refunded.SettledAt, finished.SettledAt)
			require.Equal(t, service.VideoRelayRefunded, finished.BillingState)
			require.False(t, finished.CanDownload())
			assertVideoRelayFunds(t, integrationDB, task, 10, 0, 0)
		})
	}
}

func TestVideoRelayRepoRefundDoesNotReadChargeConfiguration(t *testing.T) {
	ctx := context.Background()
	task := newVideoRelayTask(t, 10, 5)
	repo := NewVideoRelayRepository(integrationDB)
	require.NoError(t, repo.Reserve(ctx, &task))
	sent, err := repo.AcquireSend(ctx, &task)
	require.NoError(t, err)
	known, err := repo.RecordSubmission(ctx, sent, "refund-fixture-request", "refund-fixture-video")
	require.NoError(t, err)

	// PostgreSQL accepts this old row. Its charging configuration cannot be
	// decoded into current account fields and is irrelevant to releasing a hold.
	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET extra=extra || '{"fixture_unused":1e400}'::jsonb WHERE id=$1`, task.AccountID)
	require.NoError(t, err)
	finished, result, err := repo.Settle(ctx, known, service.VideoRelaySettlement{
		FailureReason: service.VideoRelayUpstreamFailed,
	})
	require.NoError(t, err, "refund must not depend on decoding charge configuration")
	require.NotNil(t, result)
	require.True(t, result.Applied)
	require.Equal(t, service.VideoRelayRefunded, finished.BillingState)
	require.Equal(t, service.VideoRelayUpstreamFailed, finished.FailureReason)
	require.False(t, finished.CanDownload())
	assertVideoRelayFunds(t, integrationDB, task, 10, 0, 0)
}
