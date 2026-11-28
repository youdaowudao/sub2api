package repository

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newScheduleTimerTestRepo(t *testing.T) (*accountRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	return newAccountRepositoryWithSQL(client, db, nil), mock
}

func expectScheduleTimerLock(mock sqlmock.Sqlmock, id int64, enabled bool, until any, now time.Time) {
	mock.ExpectQuery(`(?s)SELECT schedulable, schedule_resume_at, NOW\(\).*FOR UPDATE`).
		WithArgs(id).WillReturnRows(sqlmock.NewRows([]string{"schedulable", "schedule_resume_at", "now"}).AddRow(enabled, until, now))
}

func expectScheduleTimerSwitch(mock sqlmock.Sqlmock, enabled bool, id int64) {
	query := `UPDATE "accounts" SET .*"schedulable" = .*`
	if enabled {
		query = `UPDATE "accounts" SET (.*"schedulable" = .*"schedule_resume_at"\s*=\s*NULL|.*"schedule_resume_at"\s*=\s*NULL.*"schedulable" = .*)`
	}
	mock.ExpectExec(query).
		WithArgs(sqlmock.AnyArg(), enabled, id).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO scheduler_outbox`).
		WithArgs(service.SchedulerOutboxEventAccountChanged, id, nil, nil, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
}

func TestStartAccountScheduleTimer(t *testing.T) {
	for _, hours := range []int{1, 2, 3, 6, 12, 24} {
		t.Run(strconv.Itoa(hours)+" hours", func(t *testing.T) {
			repo, mock := newScheduleTimerTestRepo(t)
			now := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
			until := now.Add(time.Duration(hours) * time.Hour)
			mock.ExpectBegin()
			expectScheduleTimerLock(mock, 17, true, nil, now)
			mock.ExpectExec(`UPDATE accounts SET schedule_resume_at = \$1 WHERE id = \$2`).
				WithArgs(until, int64(17)).WillReturnResult(sqlmock.NewResult(0, 1))
			expectScheduleTimerSwitch(mock, false, 17)
			mock.ExpectCommit()
			result, err := repo.StartAccountScheduleTimer(context.Background(), 17, hours)
			require.NoError(t, err)
			require.Equal(t, int64(17), result.ID)
			require.False(t, result.Schedulable)
			require.Equal(t, until, *result.ScheduleResumeAt)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestStartAccountScheduleTimerPreservesExistingState(t *testing.T) {
	now := time.Now()
	for _, test := range []struct {
		name    string
		enabled bool
		until   any
	}{
		{name: "already closed"},
		{name: "pending timer", enabled: true, until: now.Add(time.Hour)},
		{name: "overdue timer still pending", enabled: true, until: now.Add(-time.Hour)},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, mock := newScheduleTimerTestRepo(t)
			mock.ExpectBegin()
			expectScheduleTimerLock(mock, 17, test.enabled, test.until, now)
			mock.ExpectRollback()
			result, err := repo.StartAccountScheduleTimer(context.Background(), 17, 6)
			require.Error(t, err)
			require.Nil(t, result)
			require.NoError(t, mock.ExpectationsWereMet(), "conflicts must not write the switch or deadline")
		})
	}
	for _, hours := range []int{0, 4, 30} {
		repo, mock := newScheduleTimerTestRepo(t)
		_, err := repo.StartAccountScheduleTimer(context.Background(), 17, hours)
		require.ErrorIs(t, err, service.ErrScheduleTimerDuration)
		require.NoError(t, mock.ExpectationsWereMet())
	}
}

func TestStartAccountScheduleTimerReturnsCommitFailure(t *testing.T) {
	repo, mock := newScheduleTimerTestRepo(t)
	now := time.Now()
	mock.ExpectBegin()
	expectScheduleTimerLock(mock, 17, true, nil, now)
	mock.ExpectExec(`UPDATE accounts SET schedule_resume_at`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE "accounts" SET .*"schedulable"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO scheduler_outbox`).WillReturnError(errors.New("notification write aborted transaction"))
	mock.ExpectCommit().WillReturnError(errors.New("transaction rolled back"))
	result, err := repo.StartAccountScheduleTimer(context.Background(), 17, 6)
	require.Error(t, err)
	require.Nil(t, result, "failed commit must never report a saved timer")
	require.Empty(t, infraerrors.Reason(err), "a commit error must not claim a confirmed rollback")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestStartAccountScheduleTimerRollsBackSQLFailure(t *testing.T) {
	for _, stage := range []string{"save deadline", "close switch"} {
		t.Run(stage, func(t *testing.T) {
			repo, mock := newScheduleTimerTestRepo(t)
			mock.ExpectBegin()
			expectScheduleTimerLock(mock, 17, true, nil, time.Now())
			deadlineWrite := mock.ExpectExec(`UPDATE accounts SET schedule_resume_at`)
			if stage == "save deadline" {
				deadlineWrite.WillReturnError(errors.New("deadline write failed"))
			} else {
				deadlineWrite.WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(`UPDATE "accounts" SET .*"schedulable"`).WillReturnError(errors.New("close failed"))
			}
			mock.ExpectRollback()
			result, err := repo.StartAccountScheduleTimer(context.Background(), 17, 6)
			require.Error(t, err)
			require.Nil(t, result, "a failed close must not report a saved timer")
			require.Equal(t, "ACCOUNT_SCHEDULE_TIMER_NOT_SAVED", infraerrors.Reason(err))
			require.Contains(t, infraerrors.Message(err), "本次没有更改调度或计时")
			require.Contains(t, infraerrors.Message(err), "重试")
			require.NotContains(t, infraerrors.Message(err), "write failed")
			require.NoError(t, mock.ExpectationsWereMet(), "switch and deadline must roll back together")
		})
	}
}

func TestStartAccountScheduleTimerReportsReadFailures(t *testing.T) {
	for _, stage := range []string{"begin", "read"} {
		t.Run(stage, func(t *testing.T) {
			repo, mock := newScheduleTimerTestRepo(t)
			begin := mock.ExpectBegin()
			if stage == "begin" {
				begin.WillReturnError(errors.New("database unavailable"))
			} else {
				mock.ExpectQuery(`(?s)SELECT schedulable, schedule_resume_at, NOW\(\).*FOR UPDATE`).WillReturnError(errors.New("database unavailable"))
				mock.ExpectRollback()
			}
			result, err := repo.StartAccountScheduleTimer(context.Background(), 17, 6)
			require.Nil(t, result)
			require.Equal(t, "ACCOUNT_SCHEDULE_TIMER_NOT_SAVED", infraerrors.Reason(err))
			require.Contains(t, infraerrors.Message(err), "本次没有更改调度或计时")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
	// Missing/deleted accounts remain a known 404 instead of a database failure.
	repo, mock := newScheduleTimerTestRepo(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT schedulable, schedule_resume_at, NOW\(\).*FOR UPDATE`).
		WillReturnRows(sqlmock.NewRows([]string{"schedulable", "schedule_resume_at", "now"}))
	mock.ExpectRollback()
	_, err := repo.StartAccountScheduleTimer(context.Background(), 17, 6)
	require.ErrorIs(t, err, service.ErrAccountNotFound)
	require.Equal(t, 404, infraerrors.Code(err))
	require.Contains(t, infraerrors.Message(err), "账号不存在或已删除")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestResumeDueScheduleTimersRollsBackWhenClearingDeadlineFails(t *testing.T) {
	repo, mock := newScheduleTimerTestRepo(t)
	now := time.Now()
	mock.ExpectQuery(`(?s)SELECT id FROM accounts.*schedule_resume_at <= NOW\(\)`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(17)))
	mock.ExpectBegin()
	expectScheduleTimerLock(mock, 17, false, now.Add(-time.Minute), now)
	mock.ExpectExec(`UPDATE "accounts" SET (.*"schedulable" = .*"schedule_resume_at"\s*=\s*NULL|.*"schedule_resume_at"\s*=\s*NULL.*"schedulable" = .*)`).WillReturnError(errors.New("enable and clear deadline failed"))
	mock.ExpectRollback()
	completed, err := repo.ResumeDueScheduleTimers(context.Background())
	require.Error(t, err)
	require.Zero(t, completed, "the enable must not be counted as committed")
	require.NoError(t, mock.ExpectationsWereMet(), "failed completion must retain the pending timer")
}

func TestResumeDueScheduleTimersUsesOriginalEnable(t *testing.T) {
	repo, mock := newScheduleTimerTestRepo(t)
	now := time.Now()
	mock.ExpectQuery(`(?s)SELECT id FROM accounts.*schedule_resume_at <= NOW\(\)`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(17)))
	mock.ExpectBegin()
	expectScheduleTimerLock(mock, 17, false, now.Add(-time.Minute), now)
	expectScheduleTimerSwitch(mock, true, 17)
	mock.ExpectCommit()
	completed, err := repo.ResumeDueScheduleTimers(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 1, completed)
	require.NoError(t, mock.ExpectationsWereMet(), "must persist the real switch and enqueue its original notification")
}

func TestResumeDueScheduleTimersRechecksCurrentDeadline(t *testing.T) {
	now := time.Now()
	for _, until := range []any{nil, now.Add(time.Hour)} {
		repo, mock := newScheduleTimerTestRepo(t)
		mock.ExpectQuery(`(?s)SELECT id FROM accounts.*schedule_resume_at <= NOW\(\)`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(17)))
		mock.ExpectBegin()
		expectScheduleTimerLock(mock, 17, false, until, now)
		mock.ExpectRollback()
		completed, err := repo.ResumeDueScheduleTimers(context.Background())
		require.NoError(t, err)
		require.Zero(t, completed)
		require.NoError(t, mock.ExpectationsWereMet(), "a stale candidate must not enable another timer")
	}
}

func TestResumeDueScheduleTimersContinuesAfterFailedAccount(t *testing.T) {
	repo, mock := newScheduleTimerTestRepo(t)
	now := time.Now()
	mock.ExpectQuery(`(?s)SELECT id FROM accounts.*schedule_resume_at <= NOW\(\)`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(17)).AddRow(int64(18)))
	mock.ExpectBegin()
	expectScheduleTimerLock(mock, 17, false, now.Add(-time.Minute), now)
	mock.ExpectExec(`UPDATE "accounts" SET .*"schedulable"`).WillReturnError(errors.New("enable failed"))
	mock.ExpectRollback()
	mock.ExpectBegin()
	expectScheduleTimerLock(mock, 18, false, now.Add(-time.Minute), now)
	expectScheduleTimerSwitch(mock, true, 18)
	mock.ExpectCommit()
	completed, err := repo.ResumeDueScheduleTimers(context.Background())
	require.Error(t, err)
	require.EqualValues(t, 1, completed)
	require.NoError(t, mock.ExpectationsWereMet(), "failed account retains its deadline while the next account is processed")
}
