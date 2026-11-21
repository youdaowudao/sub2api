//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func createScheduleTimerAccount(t *testing.T) (*dbent.Client, *dbent.Account) {
	t.Helper()
	client := testEntClient(t)
	account, err := client.Account.Create().SetName("schedule-timer-test").
		SetPlatform(service.PlatformOpenAI).SetType(service.AccountTypeAPIKey).
		SetSchedulable(true).Save(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = integrationDB.Exec("DELETE FROM accounts WHERE id = $1", account.ID) })
	return client, account
}

func TestScheduleTimerPersistsSwitchAndResumesAfterRestart(t *testing.T) {
	ctx := context.Background()
	client, account := createScheduleTimerAccount(t)
	cache := &schedulerCacheRecorder{}
	repo := newAccountRepositoryWithSQL(client, integrationDB, cache)
	result, err := repo.StartAccountScheduleTimer(ctx, account.ID, 6)
	require.NoError(t, err)
	require.False(t, result.Schedulable)
	require.WithinDuration(t, time.Now().Add(6*time.Hour), *result.ScheduleResumeAt, 5*time.Second)
	saved, err := client.Account.Get(ctx, account.ID)
	require.NoError(t, err)
	require.False(t, saved.Schedulable, "the actual database switch must be closed")
	require.NotNil(t, saved.ScheduleResumeAt)
	require.Len(t, cache.setAccounts, 1)
	require.False(t, cache.setAccounts[0].Schedulable, "reuse original close propagation after commit")
	require.NoError(t, cache.setCtxErr)
	_, err = repo.StartAccountScheduleTimer(ctx, account.ID, 24)
	require.Error(t, err)
	saved, err = client.Account.Get(ctx, account.ID)
	require.NoError(t, err)
	require.True(t, result.ScheduleResumeAt.Equal(*saved.ScheduleResumeAt), "a duplicate must not extend the timer")
	// Manual operations keep their original effect and do not create a second pause state.
	require.NoError(t, repo.SetSchedulable(ctx, account.ID, true))
	current, err := repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.True(t, current.IsSchedulable())
	require.NotNil(t, current.ScheduleResumeAt, "manual enable does not cancel the scheduled action")
	require.NoError(t, repo.SetSchedulable(ctx, account.ID, false))

	// Accelerate the persisted timer, then construct the worker anew as after a restart.
	_, err = integrationDB.Exec("UPDATE accounts SET schedule_resume_at = NOW() - INTERVAL '1 second' WHERE id = $1", account.ID)
	require.NoError(t, err)
	worker := service.NewAccountExpiryService(newAccountRepositoryWithSQL(client, integrationDB, nil), time.Hour)
	worker.Start()
	t.Cleanup(worker.Stop)
	require.Eventually(t, func() bool {
		row, err := client.Account.Get(ctx, account.ID)
		return err == nil && row.Schedulable && row.ScheduleResumeAt == nil
	}, 5*time.Second, 20*time.Millisecond, "a restarted backend must actually enable the saved switch")
	worker.Stop()
	current, err = repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.True(t, current.IsSchedulable(), "the ordinary selection predicate must see the enabled switch")
}

func TestScheduleTimerDoesNotClearExistingProtection(t *testing.T) {
	ctx := context.Background()
	client, account := createScheduleTimerAccount(t)
	protectedUntil := time.Now().Add(24 * time.Hour)
	_, err := client.Account.UpdateOneID(account.ID).SetTempUnschedulableUntil(protectedUntil).
		SetTempUnschedulableReason("existing protection").Save(ctx)
	require.NoError(t, err)
	repo := newAccountRepositoryWithSQL(client, integrationDB, nil)
	_, err = repo.StartAccountScheduleTimer(ctx, account.ID, 12)
	require.NoError(t, err)
	_, err = integrationDB.Exec("UPDATE accounts SET schedule_resume_at = NOW() - INTERVAL '1 second' WHERE id = $1", account.ID)
	require.NoError(t, err)
	count, err := repo.ResumeDueScheduleTimers(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	current, err := repo.GetByID(ctx, account.ID)
	require.NoError(t, err)
	require.True(t, current.Schedulable)
	require.WithinDuration(t, protectedUntil, *current.TempUnschedulableUntil, time.Microsecond)
	require.Equal(t, "existing protection", current.TempUnschedulableReason)
	require.False(t, current.IsSchedulable(), "normal protection still applies after enabling")
}

func TestScheduleTimerRollsBackWhenNotificationSQLFails(t *testing.T) {
	ctx := context.Background()
	client, account := createScheduleTimerAccount(t)
	cache := &schedulerCacheRecorder{}
	repo := newAccountRepositoryWithSQL(client, integrationDB, cache)
	installFailure := func() {
		t.Helper()
		_, err := integrationDB.Exec(fmt.Sprintf(`
			CREATE OR REPLACE FUNCTION fail_schedule_timer_outbox() RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN IF NEW.account_id = %d THEN RAISE EXCEPTION 'test notification SQL failure'; END IF; RETURN NEW; END $$;
			CREATE TRIGGER fail_schedule_timer_outbox BEFORE INSERT ON scheduler_outbox
			FOR EACH ROW EXECUTE FUNCTION fail_schedule_timer_outbox();
		`, account.ID))
		require.NoError(t, err)
	}
	removeFailure := func() {
		t.Helper()
		_, err := integrationDB.Exec("DROP TRIGGER IF EXISTS fail_schedule_timer_outbox ON scheduler_outbox; DROP FUNCTION IF EXISTS fail_schedule_timer_outbox()")
		require.NoError(t, err)
	}
	t.Cleanup(removeFailure)
	installFailure()
	result, err := repo.StartAccountScheduleTimer(ctx, account.ID, 24)
	require.Error(t, err)
	require.Nil(t, result)
	saved, err := client.Account.Get(ctx, account.ID)
	require.NoError(t, err)
	require.True(t, saved.Schedulable)
	require.Nil(t, saved.ScheduleResumeAt)
	require.Empty(t, cache.setAccounts, "a rolled-back close must not publish a snapshot")
	removeFailure()
	_, err = repo.StartAccountScheduleTimer(ctx, account.ID, 24)
	require.NoError(t, err)
	_, err = integrationDB.Exec("UPDATE accounts SET schedule_resume_at = NOW() - INTERVAL '1 second' WHERE id = $1", account.ID)
	require.NoError(t, err)
	installFailure()
	count, err := repo.ResumeDueScheduleTimers(ctx)
	require.Error(t, err)
	require.Zero(t, count)
	saved, err = client.Account.Get(ctx, account.ID)
	require.NoError(t, err)
	require.False(t, saved.Schedulable)
	require.NotNil(t, saved.ScheduleResumeAt, "failed enable must leave the persisted retry time")
	removeFailure()
	count, err = repo.ResumeDueScheduleTimers(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	saved, err = client.Account.Get(ctx, account.ID)
	require.NoError(t, err)
	require.True(t, saved.Schedulable)
	require.Nil(t, saved.ScheduleResumeAt)
}
