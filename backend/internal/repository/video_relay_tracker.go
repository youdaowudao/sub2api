package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const videoRelayTrackingEligibleSQL = `(billing_state='held' OR (
	billing_state='refunded' AND sent_at IS NOT NULL AND settled_at IS NOT NULL
	AND failure_reason IN ('unconfirmed_unresolvable', 'api_key_deleted', 'user_deleted')
	AND NOT ` + videoRelayTrustedTaskIDSQL + `))`

// A held task carries the source check from the same statement that assigns
// its lease. Actual settlement still checks the sources under their row locks.
const videoRelayTrackingRemovalSQL = `
	CASE
		WHEN video_relay_tasks.billing_state <> 'held' THEN ''
		WHEN NOT EXISTS (SELECT 1 FROM users u WHERE u.id=video_relay_tasks.user_id AND u.deleted_at IS NULL) THEN 'user_deleted'
		WHEN NOT EXISTS (SELECT 1 FROM api_keys k WHERE k.id=video_relay_tasks.api_key_id AND k.deleted_at IS NULL) THEN 'api_key_deleted'
		WHEN NOT EXISTS (SELECT 1 FROM accounts a WHERE a.id=video_relay_tasks.account_id AND a.deleted_at IS NULL) THEN 'account_deleted'
		ELSE ''
	END`

func (r *videoRelayRepository) ClaimDue(ctx context.Context) (*service.VideoRelayTask, error) {
	var sourceRemoval string
	task, err := scanVideoRelayTask(r.db.QueryRowContext(ctx, `
		UPDATE video_relay_tasks
		SET next_check_at=clock_timestamp()+interval '2 minutes'
		WHERE task_id=(
			SELECT task_id FROM video_relay_tasks
			WHERE next_check_at IS NOT NULL AND next_check_at <= statement_timestamp()
				AND `+videoRelayTrackingEligibleSQL+`
			ORDER BY next_check_at,task_id
			LIMIT 1 FOR UPDATE SKIP LOCKED
		) RETURNING `+videoRelayColumns+`,`+videoRelayTrackingRemovalSQL), &sourceRemoval)
	if errors.Is(err, service.ErrVideoRelayNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	task.TrackingSourceRemoval = sourceRemoval
	task.TrackingLease = task.NextCheckAt
	task.TrackingStartedAt = task.NextCheckAt.Add(-service.VideoRelayTrackingLease)
	return task, nil
}

func lockObservedVideoRelayTask(ctx context.Context, tx *sql.Tx, expected *service.VideoRelayTask) (*service.VideoRelayTask, time.Time, error) {
	var now time.Time
	current, err := scanVideoRelayTask(tx.QueryRowContext(ctx, `
		WITH locked AS MATERIALIZED (
			SELECT `+videoRelayColumns+` FROM video_relay_tasks WHERE task_id=$1 FOR UPDATE
		)
		SELECT locked.*,clock_timestamp() FROM locked`, expected.TaskID), &now)
	if err != nil {
		return nil, time.Time{}, err
	}
	if expected.TrackingLease != nil {
		if current.NextCheckAt == nil || !current.NextCheckAt.Equal(*expected.TrackingLease) ||
			!now.Before(*expected.TrackingLease) {
			return current, now, service.ErrVideoRelayLeaseLost
		}
		current.TrackingLease = expected.TrackingLease
		current.TrackingStartedAt = expected.TrackingStartedAt
	}
	return current, now, nil
}

func (r *videoRelayRepository) FinishClaim(ctx context.Context, claim *service.VideoRelayTask, finish service.VideoRelayTrackingFinish) (*service.VideoRelayTask, error) {
	if claim.TrackingLease == nil {
		return nil, service.ErrVideoRelayLeaseLost
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	current, now, err := lockObservedVideoRelayTask(ctx, tx, claim)
	if err != nil {
		return current, err
	}
	next := service.VideoRelayNextCheck(current, claim.TrackingStartedAt, now, finish.Retry)
	if current.CanTrackRefund() && finish.AccountRemoved {
		next = nil
	}
	if current.BillingState == service.VideoRelayHeld &&
		current.UpstreamTaskID == claim.UpstreamTaskID && service.ValidVideoRelayUpstreamTaskID(current.UpstreamTaskID) &&
		(finish.Status == service.VideoRelayStatusQueued || finish.Status == service.VideoRelayStatusInProgress) {
		current.Status, current.Progress = finish.Status, finish.Progress
	}
	// Keep the original lease in the UPDATE predicate as well. A delayed
	// statement can't commit progress after its lease expires.
	updated, err := scanVideoRelayTask(tx.QueryRowContext(ctx, `
		UPDATE video_relay_tasks
		SET next_check_at=$2,status=$3,progress=$4,
			updated_at=CASE WHEN status<>$3 OR progress<>$4 THEN clock_timestamp() ELSE updated_at END
		WHERE task_id=$1 AND next_check_at=$5 AND next_check_at>clock_timestamp()
		RETURNING `+videoRelayColumns, current.TaskID, next, current.Status, current.Progress, claim.TrackingLease))
	if errors.Is(err, service.ErrVideoRelayNotFound) {
		return current, service.ErrVideoRelayLeaseLost
	}
	if err != nil {
		return current, err
	}
	if err := tx.Commit(); err != nil {
		return current, err
	}
	return updated, nil
}

func lockVideoRelayBillingSources(ctx context.Context, tx *sql.Tx, task *service.VideoRelayTask, readChargeFields bool) (service.VideoRelayBillingSources, string, error) {
	var sources service.VideoRelayBillingSources
	sources.User.ID = task.UserID
	sources.Key.ID = task.APIKeyID
	sources.Account.ID = task.AccountID

	var deleted bool
	var notifyEmails string
	userFields := `deleted_at IS NOT NULL`
	userDest := []any{&deleted}
	if readChargeFields {
		userFields += `,email,COALESCE(username,''),balance,total_recharged,balance_notify_enabled,
			balance_notify_threshold_type,balance_notify_threshold,balance_notify_extra_emails`
		userDest = append(userDest, &sources.User.Email, &sources.User.Username, &sources.User.Balance,
			&sources.User.TotalRecharged, &sources.User.BalanceNotifyEnabled, &sources.User.BalanceNotifyThresholdType,
			&sources.User.BalanceNotifyThreshold, &notifyEmails)
	}
	err := tx.QueryRowContext(ctx, `SELECT `+userFields+`
		FROM users WHERE id=$1 FOR NO KEY UPDATE NOWAIT`, task.UserID).Scan(userDest...)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return sources, "", service.ErrUserNotFound
	case err != nil:
		return sources, "", err
	case deleted:
		return sources, service.VideoRelayUserDeleted, nil
	}
	if readChargeFields {
		sources.User.BalanceNotifyExtraEmails = service.ParseNotifyEmails(notifyEmails)
	}

	keyFields := `deleted_at IS NOT NULL,user_id`
	keyDest := []any{&deleted, &sources.Key.UserID}
	if readChargeFields {
		keyFields += `,quota,rate_limit_5h,rate_limit_1d,rate_limit_7d`
		keyDest = append(keyDest, &sources.Key.Quota, &sources.Key.RateLimit5h,
			&sources.Key.RateLimit1d, &sources.Key.RateLimit7d)
	}
	err = tx.QueryRowContext(ctx, `SELECT `+keyFields+`
		FROM api_keys WHERE id=$1 FOR NO KEY UPDATE NOWAIT`, task.APIKeyID).Scan(keyDest...)
	switch {
	case errors.Is(err, sql.ErrNoRows), err == nil && deleted:
		return sources, service.VideoRelayAPIKeyDeleted, nil
	case err != nil:
		return sources, "", err
	}
	if sources.Key.UserID != task.UserID {
		return sources, "", errors.New("video task original key owner mismatch")
	}

	var extra []byte
	accountFields := `deleted_at IS NOT NULL`
	accountDest := []any{&deleted}
	if readChargeFields {
		accountFields += `,name,platform,type,rate_multiplier,extra`
		accountDest = append(accountDest, &sources.Account.Name, &sources.Account.Platform,
			&sources.Account.Type, &sources.Account.RateMultiplier, &extra)
	}
	err = tx.QueryRowContext(ctx, `SELECT `+accountFields+`
		FROM accounts WHERE id=$1 FOR NO KEY UPDATE NOWAIT`, task.AccountID).Scan(accountDest...)
	switch {
	case errors.Is(err, sql.ErrNoRows), err == nil && deleted:
		return sources, service.VideoRelayAccountDeleted, nil
	case err != nil:
		return sources, "", err
	}
	if len(extra) > 0 {
		err = json.Unmarshal(extra, &sources.Account.Extra)
	}
	return sources, "", err
}

func refundDeletedVideoRelayUser(ctx context.Context, tx *sql.Tx, task *service.VideoRelayTask) (*float64, error) {
	if task.HoldAmount <= 0 {
		return nil, nil
	}
	var balance float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users SET balance=balance+$1,frozen_balance=frozen_balance-$1,updated_at=NOW()
		WHERE id=$2 AND deleted_at IS NOT NULL AND frozen_balance >= $1
		RETURNING balance`, task.HoldAmount, task.UserID).Scan(&balance)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("deleted video user has insufficient frozen funds")
	}
	if err != nil {
		return nil, err
	}
	return &balance, nil
}

const videoRelayAdminProjectionSQL = `
	WITH projected AS (
		SELECT task_id,user_id,group_id,account_id,model,upstream_model,seconds,resolution,hold_amount,
			CASE
				WHEN billing_state='charged' THEN 'completed'
				WHEN billing_state='refunded' THEN 'failed'
				WHEN sent_at IS NOT NULL AND NOT ` + videoRelayTrustedTaskIDSQL + ` THEN 'confirmation'
				ELSE 'processing'
			END AS display_status,
			billing_state,
			CASE WHEN failure_reason IN ('not_sent','upstream_rejected','upstream_failed','unconfirmed_unresolvable',
				'tracking_expired','account_deleted','api_key_deleted','user_deleted') THEN failure_reason ELSE '' END AS failure_reason,
			CASE WHEN ` + videoRelayTrustedTaskIDSQL + ` THEN upstream_task_id ELSE '' END AS upstream_task_id,
			created_at,settled_at,
			billing_state='refunded' AND sent_at IS NOT NULL AND failure_reason IN ('upstream_rejected',
				'unconfirmed_unresolvable','tracking_expired','account_deleted','api_key_deleted','user_deleted') AS risk
		FROM video_relay_tasks WHERE ($1::bigint=0 OR user_id=$1) AND ($2='' OR task_id=$2)
	)
`

const videoRelayAdminFilterSQL = ` WHERE ($3='' OR p.display_status=$3) AND (NOT $4 OR p.risk)`

func (r *videoRelayRepository) ListAdmin(ctx context.Context, filter service.VideoRelayAdminFilter, offset, limit int) ([]service.VideoRelayAdminRow, int64, error) {
	args := []any{filter.UserID, filter.TaskID, filter.Status, filter.RiskOnly}
	rows, err := r.db.QueryContext(ctx, videoRelayAdminProjectionSQL+`
		SELECT p.*,COALESCE(u.email,''),COALESCE(u.username,''),COALESCE(a.name,''),
			COALESCE(a.deleted_at IS NOT NULL,false),COALESCE(g.name,'')
		FROM (SELECT p.* FROM projected p`+videoRelayAdminFilterSQL+`
			ORDER BY p.created_at DESC,p.task_id DESC LIMIT $5 OFFSET $6) p
		LEFT JOIN users u ON u.id=p.user_id AND u.deleted_at IS NULL
		LEFT JOIN accounts a ON a.id=p.account_id
		LEFT JOIN groups g ON g.id=p.group_id
		ORDER BY p.created_at DESC,p.task_id DESC`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]service.VideoRelayAdminRow, 0, limit)
	for rows.Next() {
		var item service.VideoRelayAdminRow
		err := rows.Scan(&item.TaskID, &item.UserID, &item.GroupID, &item.AccountID, &item.Model, &item.UpstreamModel,
			&item.Seconds, &item.Resolution, &item.HoldAmount, &item.Status, &item.BillingState, &item.FailureReason,
			&item.UpstreamTaskID, &item.CreatedAt, &item.SettledAt, &item.Risk, &item.UserEmail, &item.UserName, &item.AccountName, &item.AccountDeleted, &item.GroupName)
		if err != nil {
			return nil, 0, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(result) < limit && (offset == 0 || len(result) > 0) {
		return result, int64(offset + len(result)), nil
	}
	var total int64
	err = r.db.QueryRowContext(ctx, videoRelayAdminProjectionSQL+
		"SELECT count(*) FROM projected p"+videoRelayAdminFilterSQL, args...).Scan(&total)
	return result, total, err
}
