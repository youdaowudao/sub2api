package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type lockedScheduleTimer struct {
	schedulable bool
	resumeAt    sql.NullTime
	now         time.Time
}

func scheduleTimerNotSaved(step string, cause error) error {
	return infraerrors.ServiceUnavailable("ACCOUNT_SCHEDULE_TIMER_NOT_SAVED",
		"数据库操作失败，未能"+step+"；本次没有更改调度或计时，请稍后重试").WithCause(cause)
}

func lockScheduleTimer(ctx context.Context, tx *dbent.Tx, id int64) (lockedScheduleTimer, error) {
	var state lockedScheduleTimer
	rows, err := tx.Client().QueryContext(ctx, `
		SELECT schedulable, schedule_resume_at, NOW()
		FROM accounts WHERE id = $1 AND deleted_at IS NULL FOR UPDATE
	`, id)
	if err != nil {
		return state, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return state, err
		}
		return state, service.ErrAccountNotFound
	}
	err = rows.Scan(&state.schedulable, &state.resumeAt, &state.now)
	return state, err
}

func (r *accountRepository) StartAccountScheduleTimer(ctx context.Context, id int64, hours int) (*service.AccountScheduleTimerResult, error) {
	if hours != 1 && hours != 2 && hours != 3 && hours != 6 && hours != 12 && hours != 24 {
		return nil, service.ErrScheduleTimerDuration
	}
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, scheduleTimerNotSaved("开始设置计时", err)
	}
	defer func() { _ = tx.Rollback() }()
	state, err := lockScheduleTimer(ctx, tx, id)
	if err != nil {
		if errors.Is(err, service.ErrAccountNotFound) {
			return nil, infraerrors.NotFound("ACCOUNT_NOT_FOUND", "账号不存在或已删除；本次没有更改调度，请刷新后重试").WithCause(err)
		}
		return nil, scheduleTimerNotSaved("读取账号状态", err)
	}
	if state.resumeAt.Valid {
		return nil, infraerrors.Conflict("ACCOUNT_SCHEDULE_TIMER_ACTIVE", "账号已设置自动开启时间，请刷新后查看；本次没有更改开关或计时").
			WithMetadata(map[string]string{"schedule_resume_at": state.resumeAt.Time.UTC().Format(time.RFC3339Nano)})
	}
	if !state.schedulable {
		return nil, service.ErrScheduleTimerDisabled
	}
	resumeAt := state.now.Add(time.Duration(hours) * time.Hour)
	if _, err := tx.Client().ExecContext(ctx, "UPDATE accounts SET schedule_resume_at = $1 WHERE id = $2", resumeAt, id); err != nil {
		return nil, scheduleTimerNotSaved("保存自动开启时间", err)
	}
	if err := r.SetSchedulable(dbent.NewTxContext(ctx, tx), id, false); err != nil {
		return nil, scheduleTimerNotSaved("关闭账号调度", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	// The original request context has no reference to the completed transaction.
	r.syncSchedulerAccountSnapshotDetached(ctx, id)
	return &service.AccountScheduleTimerResult{ID: id, Schedulable: false, ScheduleResumeAt: &resumeAt}, nil
}

func (r *accountRepository) ResumeDueScheduleTimers(ctx context.Context) (int64, error) {
	rows, err := r.client.QueryContext(ctx, `
		SELECT id FROM accounts
		WHERE deleted_at IS NULL AND schedule_resume_at <= NOW()
		ORDER BY id
	`)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return 0, err
	}
	var completed int64
	var firstErr error
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return completed, err
		}
		changed, err := r.resumeScheduleTimer(ctx, id)
		if err != nil {
			logger.LegacyPrintf("repository.account", "[ScheduleTimer] enable failed: account=%d err=%v", id, err)
			if firstErr == nil {
				firstErr = fmt.Errorf("enable scheduled account %d: %w", id, err)
			}
			continue
		}
		if changed {
			completed++
		}
	}
	return completed, firstErr
}

func (r *accountRepository) resumeScheduleTimer(ctx context.Context, id int64) (bool, error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	state, err := lockScheduleTimer(ctx, tx, id)
	if errors.Is(err, service.ErrAccountNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !state.resumeAt.Valid || state.resumeAt.Time.After(state.now) {
		return false, nil
	}
	if err := r.SetSchedulable(dbent.NewTxContext(ctx, tx), id, true); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
