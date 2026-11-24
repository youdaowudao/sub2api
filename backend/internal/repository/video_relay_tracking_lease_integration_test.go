//go:build integration

package repository

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func makeVideoRelayTaskDue(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		"UPDATE video_relay_tasks SET next_check_at='2000-01-01 00:00 UTC' WHERE task_id=$1", id)
	require.NoError(t, err)
}

func TestVideoRelayRepoTrackingLeaseAndCustomerSettlement(t *testing.T) {
	ctx := context.Background()
	task := newVideoRelayTask(t, 10, 5)
	db2 := videoRelayIndependentDB(t)
	first := NewVideoRelayRepository(integrationDB)
	second := NewVideoRelayRepository(db2)
	require.NoError(t, first.Reserve(ctx, &task))
	sent, err := first.AcquireSend(ctx, &task)
	require.NoError(t, err)
	_, err = first.RecordSubmission(ctx, sent, "provider-request", "provider-track")
	require.NoError(t, err)
	makeVideoRelayTaskDue(t, integrationDB, task.TaskID)

	type claimResult struct {
		task *service.VideoRelayTask
		err  error
	}
	begin := make(chan struct{})
	results := make(chan claimResult, 2)
	for _, repo := range []service.VideoRelayRepository{first, second} {
		go func() {
			<-begin
			task, err := repo.ClaimDue(ctx)
			results <- claimResult{task: task, err: err}
		}()
	}
	close(begin)
	var claims []*service.VideoRelayTask
	for range 2 {
		result := <-results
		require.NoError(t, result.err)
		if result.task != nil && result.task.TaskID == task.TaskID {
			claims = append(claims, result.task)
		}
	}
	require.Len(t, claims, 1, "two connections must never own a single outstanding video lease")
	stale := claims[0]
	var interval float64
	require.NoError(t, db2.QueryRowContext(ctx, `
		SELECT EXTRACT(EPOCH FROM next_check_at-clock_timestamp()) FROM video_relay_tasks WHERE task_id=$1`,
		task.TaskID).Scan(&interval))
	require.InDelta(t, 120, interval, 3)

	makeVideoRelayTaskDue(t, db2, task.TaskID)
	fresh, err := second.ClaimDue(ctx)
	require.NoError(t, err)
	require.NotNil(t, fresh)
	require.Equal(t, task.TaskID, fresh.TaskID)
	_, err = first.FinishClaim(ctx, stale, service.VideoRelayTrackingFinish{Status: service.VideoRelayStatusInProgress, Progress: 85})
	require.Error(t, err, "an expired worker must not write progress or clear a new worker's lease")
	current, err := second.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
	require.NoError(t, err)
	require.Zero(t, current.Progress)
	late, result, err := first.Settle(ctx, stale, service.VideoRelaySettlement{Completed: true})
	require.Error(t, err, "an expired worker must not settle funds")
	require.Nil(t, result)
	if late != nil {
		require.Equal(t, service.VideoRelayHeld, late.BillingState)
	}
	assertVideoRelayFunds(t, db2, task, 5, 5, 0)

	settled, result, err := second.Settle(ctx, current, service.VideoRelaySettlement{Completed: true})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, service.VideoRelayCharged, settled.BillingState)
	updated, err := second.FinishClaim(ctx, fresh, service.VideoRelayTrackingFinish{Status: service.VideoRelayStatusInProgress, Progress: 80})
	require.NoError(t, err)
	require.Equal(t, service.VideoRelayCharged, updated.BillingState)
	require.Equal(t, 100, updated.Progress)
	var cleared bool
	require.NoError(t, db2.QueryRowContext(ctx, "SELECT next_check_at IS NULL FROM video_relay_tasks WHERE task_id=$1", task.TaskID).Scan(&cleared))
	require.True(t, cleared)
	assertVideoRelayFunds(t, db2, task, 5, 0, 1)
}

func TestVideoRelayRepoNoSendRefundRacesPermissionToSubmit(t *testing.T) {
	ctx := context.Background()
	for _, age := range []time.Duration{9 * time.Minute, 11 * time.Minute} {
		task := newVideoRelayTask(t, 10, 5)
		db2 := videoRelayIndependentDB(t)
		first := NewVideoRelayRepository(integrationDB)
		second := NewVideoRelayRepository(db2)
		require.NoError(t, first.Reserve(ctx, &task))
		_, err := integrationDB.ExecContext(ctx, `
			UPDATE video_relay_tasks SET created_at=clock_timestamp()-make_interval(secs => $2) WHERE task_id=$1`,
			task.TaskID, age.Seconds())
		require.NoError(t, err)
		makeVideoRelayTaskDue(t, integrationDB, task.TaskID)
		claim, err := first.ClaimDue(ctx)
		require.NoError(t, err)
		require.NotNil(t, claim)
		require.Equal(t, task.TaskID, claim.TaskID)

		if age < 10*time.Minute {
			current, applied, err := first.Settle(ctx, claim, service.VideoRelaySettlement{FailureReason: service.VideoRelayNotSent})
			require.NoError(t, err)
			require.Nil(t, applied)
			require.Equal(t, service.VideoRelayHeld, current.BillingState)
			assertVideoRelayFunds(t, db2, task, 5, 5, 0)
			continue
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		var sendErr, refundErr error
		wg.Go(func() {
			<-start
			_, sendErr = second.AcquireSend(ctx, &task)
		})
		wg.Go(func() {
			<-start
			_, _, refundErr = first.Settle(ctx, claim, service.VideoRelaySettlement{FailureReason: service.VideoRelayNotSent})
		})
		close(start)
		wg.Wait()
		require.NoError(t, refundErr)
		current, err := first.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
		require.NoError(t, err)
		if sendErr == nil {
			require.NotNil(t, current.SentAt)
			require.Equal(t, service.VideoRelayHeld, current.BillingState)
			assertVideoRelayFunds(t, db2, task, 5, 5, 0)
		} else {
			require.ErrorIs(t, sendErr, service.ErrVideoRelayNoSend)
			require.Nil(t, current.SentAt)
			require.Equal(t, service.VideoRelayRefunded, current.BillingState)
			assertVideoRelayFunds(t, db2, task, 10, 0, 0)
		}
	}
}
