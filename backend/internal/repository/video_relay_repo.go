package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

const videoRelayColumns = `task_id, user_id, api_key_id, group_id, account_id,
	model, upstream_model, seconds, resolution, total_cost, hold_amount, rate_multiplier,
	upstream_site_fingerprint, upstream_credential_fingerprint, upstream_route_fingerprint,
	upstream_task_id, upstream_request_id, status, billing_state, progress, error_message, failure_reason,
	sent_at, last_lookup_at, created_at, updated_at, settled_at`

const videoRelayTrustedTaskIDSQL = `(upstream_task_id COLLATE "C" ~ '^[A-Za-z0-9_.-]{1,128}$' AND upstream_task_id ~ '[^.]')`

const videoRelayLookupInterval = 15 * time.Second

type videoRelayRepository struct {
	db      *sql.DB
	billing usageBillingRepository
}

func NewVideoRelayRepository(db *sql.DB) service.VideoRelayRepository {
	return &videoRelayRepository{db: db, billing: usageBillingRepository{db: db}}
}

func scanVideoRelayTask(row interface{ Scan(...any) error }) (*service.VideoRelayTask, error) {
	task := &service.VideoRelayTask{}
	err := row.Scan(
		&task.TaskID, &task.UserID, &task.APIKeyID, &task.GroupID, &task.AccountID,
		&task.Model, &task.UpstreamModel, &task.Seconds, &task.Resolution, &task.TotalCost, &task.HoldAmount, &task.RateMultiplier,
		&task.SiteFingerprint, &task.CredentialFingerprint, &task.RouteFingerprint,
		&task.UpstreamTaskID, &task.UpstreamRequestID, &task.Status, &task.BillingState, &task.Progress, &task.ErrorMessage, &task.FailureReason,
		&task.SentAt, &task.LastLookupAt, &task.CreatedAt, &task.UpdatedAt, &task.SettledAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVideoRelayNotFound
	}
	if err != nil {
		return nil, err
	}
	return task, nil
}

func (r *videoRelayRepository) Reserve(ctx context.Context, task *service.VideoRelayTask) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := reserveUsageBillingBatchImageBalance(ctx, tx, videoRelayAmount(task)); err != nil {
		return err
	}
	// The existing reserve helper returns immediately for a zero amount. Still
	// require the authenticated user to be active before storing a free order.
	if task.HoldAmount == 0 {
		if err := lockActiveVideoRelayUser(ctx, tx, task.UserID); err != nil {
			return err
		}
	}
	created, err := scanVideoRelayTask(tx.QueryRowContext(ctx, `
		INSERT INTO video_relay_tasks (
			task_id, user_id, api_key_id, group_id, account_id, model, upstream_model, seconds, resolution,
			total_cost, hold_amount, rate_multiplier, upstream_site_fingerprint,
			upstream_credential_fingerprint, upstream_route_fingerprint
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		RETURNING `+videoRelayColumns, task.TaskID, task.UserID, task.APIKeyID, task.GroupID, task.AccountID,
		task.Model, task.UpstreamModel, task.Seconds, task.Resolution, task.TotalCost,
		task.HoldAmount, task.RateMultiplier, task.SiteFingerprint, task.CredentialFingerprint, task.RouteFingerprint))
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	*task = *created
	return nil
}

func videoRelayAmount(task *service.VideoRelayTask) *service.BatchImageBalanceHoldCommand {
	return &service.BatchImageBalanceHoldCommand{
		UserID: task.UserID, APIKeyID: task.APIKeyID, HoldAmount: task.HoldAmount, ActualAmount: task.HoldAmount,
	}
}

func (r *videoRelayRepository) Get(ctx context.Context, taskID string, userID, apiKeyID int64) (*service.VideoRelayTask, error) {
	return scanVideoRelayTask(r.db.QueryRowContext(ctx, `SELECT `+videoRelayColumns+
		` FROM video_relay_tasks WHERE task_id=$1 AND user_id=$2 AND api_key_id=$3`, taskID, userID, apiKeyID))
}

func lockVideoRelaySiteCredential(ctx context.Context, tx *sql.Tx, task *service.VideoRelayTask) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('video-relay:' || $1 || ':' || $2, 0))`,
		task.SiteFingerprint, task.CredentialFingerprint)
	return err
}

func lockVideoRelayTask(ctx context.Context, tx *sql.Tx, id string) (*service.VideoRelayTask, error) {
	return scanVideoRelayTask(tx.QueryRowContext(ctx, `SELECT `+videoRelayColumns+
		` FROM video_relay_tasks WHERE task_id=$1 FOR UPDATE`, id))
}

func (r *videoRelayRepository) AcquireSend(ctx context.Context, task *service.VideoRelayTask) (*service.VideoRelayTask, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockVideoRelaySiteCredential(ctx, tx, task); err != nil {
		return nil, err
	}
	blocked, err := hasOtherVideoRelayUnresolvedSend(ctx, tx, task, false)
	if err != nil {
		return nil, err
	}
	if blocked {
		return nil, service.ErrVideoRelaySendBlocked
	}
	sent, err := scanVideoRelayTask(tx.QueryRowContext(ctx, `
		UPDATE video_relay_tasks SET sent_at=clock_timestamp(), updated_at=clock_timestamp()
		WHERE task_id=$1 AND status='submitting' AND billing_state='held' AND sent_at IS NULL
		  AND upstream_site_fingerprint=$2 AND upstream_credential_fingerprint=$3 AND upstream_route_fingerprint=$4
		RETURNING `+videoRelayColumns, task.TaskID, task.SiteFingerprint, task.CredentialFingerprint, task.RouteFingerprint))
	if errors.Is(err, service.ErrVideoRelayNotFound) {
		return nil, service.ErrVideoRelayNoSend
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return sent, nil
}

func hasOtherVideoRelayUnresolvedSend(ctx context.Context, tx *sql.Tx, task *service.VideoRelayTask, includeRejections bool) (bool, error) {
	var blocked bool
	err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM video_relay_tasks
			WHERE upstream_site_fingerprint=$1 AND upstream_credential_fingerprint=$2 AND task_id<>$3
			  AND sent_at IS NOT NULL AND NOT `+videoRelayTrustedTaskIDSQL+`
			  AND failure_reason <> 'not_sent'
			  AND ($4 OR NOT (status='failed' AND billing_state='refunded' AND failure_reason='upstream_rejected'))
		)`, task.SiteFingerprint, task.CredentialFingerprint, task.TaskID, includeRejections).Scan(&blocked)
	return blocked, err
}

func sameVideoRelaySend(task, sent *service.VideoRelayTask) bool {
	return task.SentAt != nil && sent.SentAt != nil && task.SentAt.Equal(*sent.SentAt) &&
		task.RouteFingerprint == sent.RouteFingerprint
}

func (r *videoRelayRepository) RecordSubmission(ctx context.Context, sent *service.VideoRelayTask, requestID, upstreamTaskID string) (*service.VideoRelayTask, error) {
	if upstreamTaskID != "" && !service.ValidVideoRelayUpstreamTaskID(upstreamTaskID) {
		return nil, errors.New("invalid upstream task ID")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	current, err := lockVideoRelayTask(ctx, tx, sent.TaskID)
	if err != nil {
		return nil, err
	}
	if current.BillingState != service.VideoRelayHeld || !sameVideoRelaySend(current, sent) {
		return current, nil
	}
	if current.UpstreamRequestID != "" && requestID != "" && requestID != current.UpstreamRequestID ||
		service.ValidVideoRelayUpstreamTaskID(current.UpstreamTaskID) && upstreamTaskID != "" && upstreamTaskID != current.UpstreamTaskID {
		return current, errors.New("video submission evidence conflicts with stored identity")
	}
	updated, err := scanVideoRelayTask(tx.QueryRowContext(ctx, `
		UPDATE video_relay_tasks
		SET upstream_request_id=COALESCE(NULLIF($2, ''), upstream_request_id),
		    upstream_task_id=COALESCE(NULLIF($3, ''), upstream_task_id),
		    status=CASE
		        WHEN status='submitting' AND $3='' THEN 'unconfirmed'
		        WHEN status IN ('submitting','unconfirmed') AND $3<>'' THEN 'queued'
		        ELSE status
		    END,
		    updated_at=clock_timestamp()
		WHERE task_id=$1 RETURNING `+videoRelayColumns, current.TaskID, requestID, upstreamTaskID))
	if err != nil {
		return current, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return updated, nil
}

func (r *videoRelayRepository) ClaimLookup(ctx context.Context, taskID string) (*service.VideoRelayTask, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	task, err := lockVideoRelayTask(ctx, tx, taskID)
	if err != nil {
		return nil, false, err
	}
	if !task.CanLookupSubmission() {
		return task, false, nil
	}
	var now time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, false, err
	}
	cutoff := task.SentAt.Add(service.VideoRelayConfirmationWindow)
	if last := task.LastLookupAt; last != nil && now.Sub(*last) < videoRelayLookupInterval {
		// The first lookup at or beyond the deadline may bypass the normal interval.
		if now.Before(cutoff) || !last.Before(cutoff) {
			return task, false, nil
		}
	}
	ambiguous, err := hasOtherVideoRelayUnresolvedSend(ctx, tx, task, false)
	if err != nil || ambiguous {
		return task, false, err
	}
	ticket, err := scanVideoRelayTask(tx.QueryRowContext(ctx, `
		UPDATE video_relay_tasks SET last_lookup_at=$2, updated_at=$2 WHERE task_id=$1 RETURNING `+videoRelayColumns, taskID, now))
	if err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return ticket, true, nil
}

func sameVideoRelayLookup(current, ticket *service.VideoRelayTask) bool {
	return sameVideoRelaySend(current, ticket) && current.UpstreamRequestID == ticket.UpstreamRequestID &&
		current.LastLookupAt != nil && ticket.LastLookupAt != nil && current.LastLookupAt.Equal(*ticket.LastLookupAt)
}

func (r *videoRelayRepository) ConfirmLookup(ctx context.Context, ticket *service.VideoRelayTask, collectionPath string, logs []service.VideoRelayChargeRecord) (*service.VideoRelayTask, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockVideoRelaySiteCredential(ctx, tx, ticket); err != nil {
		return nil, err
	}
	current, err := lockVideoRelayTask(ctx, tx, ticket.TaskID)
	if err != nil {
		return nil, err
	}
	if !current.CanLookupSubmission() || !sameVideoRelayLookup(current, ticket) {
		return current, nil
	}
	candidate, err := videoRelayLookupCandidate(ctx, tx, current, collectionPath, logs)
	if err != nil || candidate == "" {
		return current, err
	}
	updated, err := scanVideoRelayTask(tx.QueryRowContext(ctx, `
		UPDATE video_relay_tasks
		SET upstream_task_id=$2, updated_at=clock_timestamp(),
		    status=CASE WHEN billing_state='held' THEN 'queued' ELSE status END
		WHERE task_id=$1 RETURNING `+videoRelayColumns, current.TaskID, candidate))
	if err != nil {
		var pgError *pq.Error
		if errors.As(err, &pgError) && pgError.Constraint == "video_relay_tasks_site_task_uidx" {
			return current, nil
		}
		return current, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return updated, nil
}

func videoRelayLookupCandidate(ctx context.Context, tx *sql.Tx, current *service.VideoRelayTask, collectionPath string, logs []service.VideoRelayChargeRecord) (string, error) {
	if len(logs) == 0 || len(logs) >= 1000 {
		return "", nil
	}
	exact := current.UpstreamRequestID != ""
	ambiguous, err := hasOtherVideoRelayUnresolvedSend(ctx, tx, current, !exact)
	if err != nil || ambiguous {
		return "", err
	}
	taskIDs := make([]string, 0, len(logs))
	for _, log := range logs {
		if (!exact || log.RequestID == current.UpstreamRequestID) && service.ValidVideoRelayUpstreamTaskID(log.TaskID) {
			taskIDs = append(taskIDs, log.TaskID)
		}
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT upstream_task_id, upstream_credential_fingerprint FROM video_relay_tasks
		WHERE upstream_site_fingerprint=$1 AND upstream_task_id<>'' AND upstream_task_id=ANY($2)`,
		current.SiteFingerprint, pq.Array(taskIDs))
	if err != nil {
		return "", err
	}
	defer rows.Close()
	known := make(map[string]string, len(taskIDs))
	for rows.Next() {
		var id, credential string
		if err := rows.Scan(&id, &credential); err != nil {
			return "", err
		}
		known[id] = credential
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	var candidate string
	for _, log := range logs {
		if exact && log.RequestID != current.UpstreamRequestID {
			continue
		}
		videoPath := log.Path == collectionPath || log.Path == "/v1/videos"
		if videoPath && known[log.TaskID] == current.CredentialFingerprint && !exact {
			continue
		}
		if !videoPath || !service.ValidVideoRelayUpstreamTaskID(log.TaskID) || known[log.TaskID] != "" ||
			log.Model != current.UpstreamModel || candidate != "" && candidate != log.TaskID {
			return "", nil
		}
		candidate = log.TaskID
	}
	return candidate, nil
}

func (r *videoRelayRepository) UpdateProgress(ctx context.Context, task *service.VideoRelayTask, status string, progress int) (*service.VideoRelayTask, error) {
	if status != service.VideoRelayStatusQueued && status != service.VideoRelayStatusInProgress {
		return nil, errors.New("invalid nonterminal video state")
	}
	updated, err := scanVideoRelayTask(r.db.QueryRowContext(ctx, `
		UPDATE video_relay_tasks SET status=$2, progress=$3, updated_at=clock_timestamp()
		WHERE task_id=$1 AND billing_state='held' AND upstream_task_id=$4 AND `+videoRelayTrustedTaskIDSQL+
		` RETURNING `+videoRelayColumns, task.TaskID, status, progress, task.UpstreamTaskID))
	if errors.Is(err, service.ErrVideoRelayNotFound) {
		return r.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
	}
	return updated, err
}

func (r *videoRelayRepository) Settle(ctx context.Context, task *service.VideoRelayTask, change service.VideoRelaySettlement) (*service.VideoRelayTask, *service.UsageBillingApplyResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	current, err := lockVideoRelayTask(ctx, tx, task.TaskID)
	if err != nil {
		return nil, nil, err
	}
	if current.BillingState != service.VideoRelayHeld || !canSettleVideoRelay(current, task, change) {
		return current, nil, nil
	}
	if err := lockActiveVideoRelayUser(ctx, tx, current.UserID); err != nil {
		return current, nil, err
	}
	result := &service.UsageBillingApplyResult{Applied: true}
	reason, message := change.FailureReason, change.ErrorMessage
	status, billingState, progress := service.VideoRelayStatusFailed, service.VideoRelayRefunded, current.Progress
	if change.Completed {
		if err := r.charge(ctx, tx, current, change, result); err != nil {
			return current, nil, err
		}
		status, billingState, progress = service.VideoRelayStatusCompleted, service.VideoRelayCharged, 100
		reason, message = "", ""
	} else {
		hold, err := releaseUsageBillingHeldBalance(ctx, tx, videoRelayAmount(current))
		if err != nil {
			return current, nil, err
		}
		result.NewBalance = hold.NewBalance
	}
	finished, err := scanVideoRelayTask(tx.QueryRowContext(ctx, `
		UPDATE video_relay_tasks
		SET status=$2, billing_state=$3, progress=$4, failure_reason=$5, error_message=$6,
		    settled_at=clock_timestamp(), updated_at=clock_timestamp()
		WHERE task_id=$1 RETURNING `+videoRelayColumns, current.TaskID, status, billingState, progress, reason, message))
	if err != nil {
		return current, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return finished, result, nil
}

// These conditions are checked with the task row locked, using the persisted
// evidence for the same send or lookup that gave the caller its result.
func canSettleVideoRelay(current, expected *service.VideoRelayTask, change service.VideoRelaySettlement) bool {
	hasTaskID := service.ValidVideoRelayUpstreamTaskID(current.UpstreamTaskID)
	if change.Completed || change.FailureReason == service.VideoRelayUpstreamFailed {
		return hasTaskID && current.UpstreamTaskID == expected.UpstreamTaskID
	}
	switch change.FailureReason {
	case service.VideoRelayNotSent:
		return !hasTaskID && current.UpstreamRequestID == "" &&
			(current.SentAt == nil && expected.SentAt == nil || sameVideoRelaySend(current, expected))
	case service.VideoRelayUpstreamRejected:
		return !hasTaskID && sameVideoRelaySend(current, expected) &&
			current.UpstreamRequestID != "" && current.UpstreamRequestID == expected.UpstreamRequestID
	case service.VideoRelayUnconfirmed:
		return !hasTaskID && sameVideoRelayLookup(current, expected) &&
			!current.LastLookupAt.Before(current.SentAt.Add(service.VideoRelayConfirmationWindow))
	default:
		return false
	}
}

func lockActiveVideoRelayUser(ctx context.Context, tx *sql.Tx, id int64) error {
	var present int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM users WHERE id=$1 AND deleted_at IS NULL FOR NO KEY UPDATE`, id).Scan(&present)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrUserNotFound
	}
	return err
}

func (r *videoRelayRepository) charge(ctx context.Context, tx *sql.Tx, task *service.VideoRelayTask, change service.VideoRelaySettlement, result *service.UsageBillingApplyResult) error {
	cmd := change.Billing
	cmd.RequestID, cmd.UserID, cmd.APIKeyID, cmd.AccountID = task.UsageRequestID(), task.UserID, task.APIKeyID, task.AccountID
	cmd.AccountType, cmd.Model, cmd.BillingType = change.AccountType, task.Model, service.BillingTypeBalance
	cmd.BalanceCost, cmd.SubscriptionCost, cmd.SubscriptionID = 0, 0, nil
	cmd.RequestFingerprint = ""
	cmd.Normalize()
	applied, err := r.billing.claimUsageBillingKey(ctx, tx, &cmd)
	if err != nil {
		return err
	}
	if !applied {
		return fmt.Errorf("video task %s is still held with an existing usage billing key", task.TaskID)
	}
	hold, err := captureUsageBillingBatchImageBalance(ctx, tx, videoRelayAmount(task))
	if err != nil {
		return err
	}
	result.NewBalance = hold.NewBalance
	if err := r.billing.applyUsageBillingEffects(ctx, tx, &cmd, result); err != nil {
		return err
	}
	endpoint, mode := "/v1/videos", string(service.BillingModeVideo)
	log := &service.UsageLog{
		RequestID: task.UsageRequestID(), UserID: task.UserID, APIKeyID: task.APIKeyID, AccountID: task.AccountID,
		GroupID: &task.GroupID, Model: task.Model, RequestedModel: task.Model, UpstreamModel: &task.UpstreamModel,
		VideoCount: 1, VideoResolution: &task.Resolution, VideoDurationSeconds: &task.Seconds,
		TotalCost: task.TotalCost, ActualCost: task.HoldAmount, RateMultiplier: task.RateMultiplier,
		BillingType: service.BillingTypeBalance, BillingMode: &mode, RequestType: service.RequestTypeSync,
		InboundEndpoint: &endpoint, UpstreamEndpoint: &endpoint,
	}
	inserted, err := insertSingleUsageLog(ctx, tx, log)
	if err != nil {
		return err
	}
	if !inserted {
		return fmt.Errorf("video task %s is still held with an existing usage record", task.TaskID)
	}
	return nil
}
