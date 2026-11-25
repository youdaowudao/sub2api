//go:build integration

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/server/routes"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func newVideoRelayTask(t *testing.T, balance, hold float64) service.VideoRelayTask {
	t.Helper()
	client := testEntClient(t)
	tag := uuid.NewString()
	user := mustCreateUser(t, client, &service.User{
		Email: "video-" + tag + "@example.com", PasswordHash: "hash", Balance: balance,
	})
	group := mustCreateGroup(t, client, &service.Group{Name: "video-" + tag, Platform: service.PlatformOpenAI})
	key := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID, Key: "video-fixture-" + tag, Name: "video", Quota: 30, GroupID: &group.ID,
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name: "video-" + tag, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Extra: map[string]any{"quota_limit": 30.0},
	})
	return service.VideoRelayTask{
		TaskID: videoRelayFixtureID(), UserID: user.ID, APIKeyID: key.ID, GroupID: group.ID, AccountID: account.ID,
		Model: "video-public", UpstreamModel: "fixture-model-480p", Seconds: 30, Resolution: "480p",
		TotalCost: 5, HoldAmount: hold, RateMultiplier: hold / 5,
		SiteFingerprint:       strings.ReplaceAll(tag+tag, "-", ""),
		CredentialFingerprint: strings.Repeat("b", 64), RouteFingerprint: strings.Repeat("c", 64),
	}
}

func videoRelayFixtureID() string {
	return "video_" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

func videoRelayIndependentDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("postgres", integrationDSN)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

func assertVideoRelayFunds(t *testing.T, db *sql.DB, task service.VideoRelayTask, balance, frozen float64, usageCount int) {
	t.Helper()
	ctx := context.Background()
	var actualBalance, actualFrozen float64
	require.NoError(t, db.QueryRowContext(ctx, "SELECT balance, frozen_balance FROM users WHERE id=$1", task.UserID).Scan(&actualBalance, &actualFrozen))
	require.InDelta(t, balance, actualBalance, 0.00000001)
	require.InDelta(t, frozen, actualFrozen, 0.00000001)
	for _, table := range []string{"usage_logs", "usage_billing_dedup"} {
		var count int
		query := "SELECT COUNT(*) FROM " + table + " WHERE request_id=$1 AND api_key_id=$2"
		require.NoError(t, db.QueryRowContext(ctx, query, task.UsageRequestID(), task.APIKeyID).Scan(&count))
		require.Equal(t, usageCount, count, table)
	}
}

func TestVideoRelayRepoConcurrentReserveAndInsertRollback(t *testing.T) {
	ctx := context.Background()
	task := newVideoRelayTask(t, 5, 5)
	other := task
	other.TaskID = videoRelayFixtureID()
	other.CredentialFingerprint = strings.Repeat("d", 64)
	secondDB := videoRelayIndependentDB(t)
	repos := []service.VideoRelayRepository{NewVideoRelayRepository(integrationDB), NewVideoRelayRepository(secondDB)}

	start := make(chan struct{})
	done := make(chan error, 2)
	for i, candidate := range []*service.VideoRelayTask{&task, &other} {
		go func() {
			<-start
			done <- repos[i].Reserve(ctx, candidate)
		}()
	}
	close(start)
	successes := 0
	for range 2 {
		if err := <-done; err == nil {
			successes++
		} else {
			require.ErrorIs(t, err, service.ErrBatchImageInsufficientBalance)
		}
	}
	require.Equal(t, 1, successes)
	var count int
	require.NoError(t, secondDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM video_relay_tasks WHERE user_id=$1", task.UserID).Scan(&count))
	require.Equal(t, 1, count)
	assertVideoRelayFunds(t, secondDB, task, 0, 5, 0)

	// With sufficient funds, a primary key collision must still roll back the hold.
	collision := newVideoRelayTask(t, 10, 5)
	require.NoError(t, repos[0].Reserve(ctx, &collision))
	require.Error(t, repos[1].Reserve(ctx, &collision))
	assertVideoRelayFunds(t, secondDB, collision, 5, 5, 0)
}

func TestVideoRelayRepoSendAndLookupAreExclusiveAcrossConnections(t *testing.T) {
	ctx := context.Background()
	a := newVideoRelayTask(t, 20, 5)
	b := a
	b.TaskID = videoRelayFixtureID()
	db2 := videoRelayIndependentDB(t)
	repo1, repo2 := NewVideoRelayRepository(integrationDB), NewVideoRelayRepository(db2)
	require.NoError(t, repo1.Reserve(ctx, &a))
	require.NoError(t, repo2.Reserve(ctx, &b))

	start := make(chan struct{})
	type attempt struct {
		request service.VideoRelayTask
		sent    *service.VideoRelayTask
		err     error
	}
	results := make(chan attempt, 2)
	for i, task := range []service.VideoRelayTask{a, b} {
		repo := []service.VideoRelayRepository{repo1, repo2}[i]
		go func() {
			<-start
			sent, err := repo.AcquireSend(ctx, &task)
			results <- attempt{task, sent, err}
		}()
	}
	close(start)
	success, blocked := <-results, <-results
	if success.err != nil {
		success, blocked = blocked, success
	}
	require.NoError(t, success.err)
	require.NotNil(t, success.sent.SentAt)
	require.ErrorIs(t, blocked.err, service.ErrVideoRelaySendBlocked)
	unclaimed, err := repo1.Get(ctx, blocked.request.TaskID, b.UserID, b.APIKeyID)
	require.NoError(t, err)
	require.Nil(t, unclaimed.SentAt)
	refunded, _, err := repo2.Settle(ctx, unclaimed, service.VideoRelaySettlement{FailureReason: service.VideoRelayNotSent})
	require.NoError(t, err)
	require.Equal(t, service.VideoRelayRefunded, refunded.BillingState)
	assertVideoRelayFunds(t, db2, a, 15, 5, 0)

	sent, err := repo2.RecordSubmission(ctx, success.sent, "reply-r1", "")
	require.NoError(t, err)
	require.Equal(t, "reply-r1", sent.UpstreamRequestID)
	ticket, claimed, err := repo1.ClaimLookup(ctx, sent.TaskID)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotNil(t, ticket)
	redundant, claimed, err := repo2.ClaimLookup(ctx, sent.TaskID)
	require.NoError(t, err)
	require.False(t, claimed)
	require.Equal(t, ticket.TaskID, redundant.TaskID)

	rogue := service.VideoRelayChargeRecord{RequestID: "reply-r2", TaskID: "wrong-video", Model: sent.UpstreamModel, Path: "/v1/videos"}
	unchanged, err := repo2.ConfirmLookup(ctx, ticket, "/v1/videos", []service.VideoRelayChargeRecord{rogue})
	require.NoError(t, err)
	require.Empty(t, unchanged.UpstreamTaskID)
	require.Equal(t, service.VideoRelayHeld, unchanged.BillingState)

	// Move only the stored lookup time first. The old result must be rejected
	// even though the send attempt, request ID and all other fields still match.
	_, err = db2.ExecContext(ctx, "UPDATE video_relay_tasks SET last_lookup_at=clock_timestamp()-interval '16 seconds' WHERE task_id=$1", sent.TaskID)
	require.NoError(t, err)
	latest, claimed, err := repo2.ClaimLookup(ctx, sent.TaskID)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotNil(t, latest)
	right := service.VideoRelayChargeRecord{RequestID: "reply-r1", TaskID: "right-video", Model: sent.UpstreamModel, Path: "/v1/videos"}
	stale, err := repo1.ConfirmLookup(ctx, ticket, "/v1/videos", []service.VideoRelayChargeRecord{right})
	require.NoError(t, err)
	require.Empty(t, stale.UpstreamTaskID)

	_, err = db2.ExecContext(ctx, `
		UPDATE video_relay_tasks
		SET sent_at=clock_timestamp()-interval '30 minutes', last_lookup_at=clock_timestamp()-interval '1 second'
		WHERE task_id=$1`, sent.TaskID)
	require.NoError(t, err)
	final, claimed, err := repo2.ClaimLookup(ctx, sent.TaskID)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotNil(t, final, "first deadline lookup must bypass the normal interval")
	expired, _, err := repo2.Settle(ctx, final, service.VideoRelaySettlement{FailureReason: service.VideoRelayUnconfirmed})
	require.NoError(t, err)
	require.Equal(t, service.VideoRelayRefunded, expired.BillingState)
	assertVideoRelayFunds(t, db2, a, 20, 0, 0)
	next := a
	next.TaskID = videoRelayFixtureID()
	require.NoError(t, repo1.Reserve(ctx, &next))
	_, err = repo1.AcquireSend(ctx, &next)
	require.ErrorIs(t, err, service.ErrVideoRelaySendBlocked)

	_, err = db2.ExecContext(ctx, "UPDATE video_relay_tasks SET last_lookup_at=clock_timestamp()-interval '16 seconds' WHERE task_id=$1", sent.TaskID)
	require.NoError(t, err)
	recovery, claimed, err := repo1.ClaimLookup(ctx, sent.TaskID)
	require.NoError(t, err)
	require.True(t, claimed)
	recovered, err := repo2.ConfirmLookup(ctx, recovery, "/v1/videos", []service.VideoRelayChargeRecord{right})
	require.NoError(t, err)
	require.Equal(t, "right-video", recovered.UpstreamTaskID)
	require.Equal(t, service.VideoRelayRefunded, recovered.BillingState)
	require.Equal(t, service.VideoRelayStatusFailed, recovered.Status)
	assertVideoRelayFunds(t, db2, a, 15, 5, 0)
	_, err = repo1.AcquireSend(ctx, &next)
	require.NoError(t, err)
}

func TestVideoRelayRepoRejectedSubmissionDoesNotClaimLateCharge(t *testing.T) {
	ctx := context.Background()
	task := newVideoRelayTask(t, 10, 5)
	db2 := videoRelayIndependentDB(t)
	repo1, repo2 := NewVideoRelayRepository(integrationDB), NewVideoRelayRepository(db2)
	require.NoError(t, repo1.Reserve(ctx, &task))
	sent, err := repo1.AcquireSend(ctx, &task)
	require.NoError(t, err)
	evidence, err := repo1.RecordSubmission(ctx, sent, "rejected", "")
	require.NoError(t, err)
	rejected, _, err := repo1.Settle(ctx, evidence, service.VideoRelaySettlement{FailureReason: service.VideoRelayUpstreamRejected})
	require.NoError(t, err)
	require.Equal(t, service.VideoRelayRefunded, rejected.BillingState)
	require.Equal(t, service.VideoRelayUpstreamRejected, rejected.FailureReason)
	assertVideoRelayFunds(t, db2, task, 10, 0, 0)
	ticket, claimed, err := repo2.ClaimLookup(ctx, rejected.TaskID)
	require.NoError(t, err)
	require.False(t, claimed)
	require.Equal(t, rejected.TaskID, ticket.TaskID)

	next := task
	next.TaskID = videoRelayFixtureID()
	require.NoError(t, repo2.Reserve(ctx, &next))
	nextSent, err := repo2.AcquireSend(ctx, &next)
	require.NoError(t, err)
	ticket, claimed, err = repo2.ClaimLookup(ctx, next.TaskID)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotNil(t, ticket)
	logs := []service.VideoRelayChargeRecord{
		{RequestID: "rejected", TaskID: "late-old-video", Path: "/v1/videos", Model: task.UpstreamModel},
	}
	nextUnknown, err := repo2.ConfirmLookup(ctx, ticket, "/v1/videos", logs)
	require.NoError(t, err)
	require.Empty(t, nextUnknown.UpstreamTaskID, "an unnumbered submission cannot claim a late charge after a rejection")
	assertVideoRelayFunds(t, db2, next, 5, 5, 0)

	nextSent, err = repo2.RecordSubmission(ctx, nextSent, "second-request", "")
	require.NoError(t, err)
	_, err = db2.ExecContext(ctx, "UPDATE video_relay_tasks SET last_lookup_at=clock_timestamp()-interval '16 seconds' WHERE task_id=$1", next.TaskID)
	require.NoError(t, err)
	ticket, claimed, err = repo2.ClaimLookup(ctx, nextSent.TaskID)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotNil(t, ticket)
	logs = append(logs, service.VideoRelayChargeRecord{
		RequestID: "second-request", TaskID: "second-video", Path: "/v1/videos", Model: task.UpstreamModel,
	})
	confirmed, err := repo2.ConfirmLookup(ctx, ticket, "/v1/videos", logs)
	require.NoError(t, err)
	require.Equal(t, "second-video", confirmed.UpstreamTaskID)
	original, err := repo2.Get(ctx, rejected.TaskID, rejected.UserID, rejected.APIKeyID)
	require.NoError(t, err)
	require.Empty(t, original.UpstreamTaskID)
	require.Equal(t, service.VideoRelayUpstreamRejected, original.FailureReason)
}

func TestVideoRelayRepoPreDeadlineTicketCannotRefundAfterDeadline(t *testing.T) {
	ctx := context.Background()
	task := newVideoRelayTask(t, 10, 5)
	db := videoRelayIndependentDB(t)
	reads, writes := NewVideoRelayRepository(db), NewVideoRelayRepository(integrationDB)
	require.NoError(t, writes.Reserve(ctx, &task))
	_, err := writes.AcquireSend(ctx, &task)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE video_relay_tasks SET sent_at=clock_timestamp()-interval '29 minutes 59 seconds'
		WHERE task_id=$1`, task.TaskID)
	require.NoError(t, err)
	ticket, claimed, err := reads.ClaimLookup(ctx, task.TaskID)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotNil(t, ticket)
	cutoff := ticket.SentAt.Add(30 * time.Minute)
	require.True(t, ticket.LastLookupAt.Before(cutoff))
	_, err = db.ExecContext(ctx, `SELECT pg_sleep_until($1::timestamptz + interval '1 second')`, cutoff)
	require.NoError(t, err)

	change := service.VideoRelaySettlement{FailureReason: service.VideoRelayUnconfirmed}
	held, result, err := writes.Settle(ctx, ticket, change)
	require.NoError(t, err)
	require.Equal(t, service.VideoRelayHeld, held.BillingState, "a qualification taken before the deadline must preserve the hold")
	require.Nil(t, result)
	assertVideoRelayFunds(t, db, task, 5, 5, 0)

	final, claimed, err := reads.ClaimLookup(ctx, task.TaskID)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotNil(t, final)
	require.False(t, final.LastLookupAt.Before(cutoff))
	refunded, _, err := writes.Settle(ctx, final, change)
	require.NoError(t, err)
	require.Equal(t, service.VideoRelayRefunded, refunded.BillingState)
	assertVideoRelayFunds(t, db, task, 10, 0, 0)
}

func TestVideoRelayRepoExactLookupRefusesAnotherRequest(t *testing.T) {
	ctx := context.Background()
	task := newVideoRelayTask(t, 10, 5)
	repo := NewVideoRelayRepository(integrationDB)
	require.NoError(t, repo.Reserve(ctx, &task))
	sent, err := repo.AcquireSend(ctx, &task)
	require.NoError(t, err)
	_, err = repo.RecordSubmission(ctx, sent, "audit-exact-request", "")
	require.NoError(t, err)
	ticket, claimed, err := repo.ClaimLookup(ctx, task.TaskID)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotNil(t, ticket)
	charge := service.VideoRelayChargeRecord{
		RequestID: "another-request", TaskID: "another-provider-video", Path: "/v1/videos", Model: task.UpstreamModel,
	}
	current, err := repo.ConfirmLookup(ctx, ticket, "/v1/videos", []service.VideoRelayChargeRecord{charge})
	require.NoError(t, err)
	require.Empty(t, current.UpstreamTaskID, "another request's paid video must never be assigned to this order")
	assertVideoRelayFunds(t, integrationDB, task, 5, 5, 0)
	charge.RequestID = "audit-exact-request"
	charge.TaskID = "audit-owned-provider-video"
	matched, err := repo.ConfirmLookup(ctx, ticket, "/v1/videos", []service.VideoRelayChargeRecord{charge})
	require.NoError(t, err)
	require.Equal(t, charge.TaskID, matched.UpstreamTaskID)
	require.Equal(t, service.VideoRelayHeld, matched.BillingState)
	assertVideoRelayFunds(t, integrationDB, task, 5, 5, 0)
}

func TestVideoRelayRepoRejectionAndConfirmedChargeAreExclusive(t *testing.T) {
	ctx := context.Background()
	task := newVideoRelayTask(t, 10, 5)
	db := videoRelayIndependentDB(t)
	rejecting, confirming := NewVideoRelayRepository(integrationDB), NewVideoRelayRepository(db)
	require.NoError(t, rejecting.Reserve(ctx, &task))
	sent, err := rejecting.AcquireSend(ctx, &task)
	require.NoError(t, err)
	sent, err = rejecting.RecordSubmission(ctx, sent, "fixture-request", "")
	require.NoError(t, err)
	ticket, claimed, err := confirming.ClaimLookup(ctx, sent.TaskID)
	require.NoError(t, err)
	require.True(t, claimed)
	require.NotNil(t, ticket)

	start, done := make(chan struct{}), make(chan error, 2)
	go func() {
		<-start
		_, _, err := rejecting.Settle(ctx, sent, service.VideoRelaySettlement{FailureReason: service.VideoRelayUpstreamRejected})
		done <- err
	}()
	go func() {
		<-start
		confirmed, err := confirming.ConfirmLookup(ctx, ticket, "/v1/videos", []service.VideoRelayChargeRecord{
			{RequestID: "fixture-request", TaskID: "right-video", Model: sent.UpstreamModel, Path: "/v1/videos"},
		})
		if err == nil && confirmed.UpstreamTaskID != "" {
			_, _, err = confirming.Settle(ctx, confirmed, service.VideoRelaySettlement{
				Completed: true, AccountType: service.AccountTypeAPIKey,
			})
		}
		done <- err
	}()
	close(start)
	require.NoError(t, <-done)
	require.NoError(t, <-done)
	final, err := confirming.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
	require.NoError(t, err)
	switch final.BillingState {
	case service.VideoRelayCharged:
		require.Equal(t, "right-video", final.UpstreamTaskID)
		require.True(t, final.CanDownload())
		assertVideoRelayFunds(t, db, task, 5, 0, 1)
	case service.VideoRelayRefunded:
		require.Empty(t, final.UpstreamTaskID)
		require.Equal(t, service.VideoRelayUpstreamRejected, final.FailureReason)
		require.False(t, final.CanDownload())
		assertVideoRelayFunds(t, db, task, 10, 0, 0)
	default:
		t.Fatalf("expected exactly one terminal billing result, got %s", final.BillingState)
	}
}

func TestVideoRelayRepoRejectedRefundWriteFailureKeepsHold(t *testing.T) {
	ctx := context.Background()
	task := newVideoRelayTask(t, 10, 5)
	db := videoRelayIndependentDB(t)
	writes, reads := NewVideoRelayRepository(integrationDB), NewVideoRelayRepository(db)
	require.NoError(t, writes.Reserve(ctx, &task))
	sent, err := writes.AcquireSend(ctx, &task)
	require.NoError(t, err)
	sent, err = writes.RecordSubmission(ctx, sent, "fixture-rejection", "")
	require.NoError(t, err)
	// Fail the final task write after the user's balance was updated.
	add := "ALTER TABLE video_relay_tasks ADD CONSTRAINT video_fixture_reject_refund CHECK (task_id <> " +
		pq.QuoteLiteral(task.TaskID) + " OR billing_state <> 'refunded')"
	_, err = db.ExecContext(ctx, add)
	require.NoError(t, err)
	const drop = "ALTER TABLE video_relay_tasks DROP CONSTRAINT IF EXISTS video_fixture_reject_refund"
	t.Cleanup(func() {
		_, err := db.ExecContext(ctx, drop)
		require.NoError(t, err)
	})
	change := service.VideoRelaySettlement{FailureReason: service.VideoRelayUpstreamRejected}
	_, _, err = writes.Settle(ctx, sent, change)
	require.Error(t, err)
	current, err := reads.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
	require.NoError(t, err)
	require.Equal(t, service.VideoRelayHeld, current.BillingState)
	require.Empty(t, current.FailureReason)
	assertVideoRelayFunds(t, db, task, 5, 5, 0)

	_, err = db.ExecContext(ctx, drop)
	require.NoError(t, err)
	refunded, _, err := writes.Settle(ctx, sent, change)
	require.NoError(t, err)
	require.Equal(t, service.VideoRelayRefunded, refunded.BillingState)
	assertVideoRelayFunds(t, db, task, 10, 0, 0)
}

func TestVideoRelayRepoUnnumberedLookupExcludesKnownOrdersAndRequiresUnambiguousPath(t *testing.T) {
	ctx := context.Background()
	db := videoRelayIndependentDB(t)
	writes, reads := NewVideoRelayRepository(integrationDB), NewVideoRelayRepository(db)
	for _, fixture := range []struct {
		name, path string
		extra      []service.VideoRelayChargeRecord
		accept     bool
	}{
		{name: "actual deployment prefix", path: "/tenant/v1/videos", accept: true},
		{name: "reverse proxy without prefix", path: "/v1/videos", accept: true},
		{name: "different tenant", path: "/other/v1/videos"},
		{name: "two unknown videos", path: "/v1/videos", extra: []service.VideoRelayChargeRecord{{TaskID: "second-new-task", Path: "/v1/videos", Model: "fixture-model-480p"}}},
		{name: "unrelated consumption", path: "/v1/videos", extra: []service.VideoRelayChargeRecord{{Path: "/v1/chat/completions"}}},
		{name: "only existing order"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			old := newVideoRelayTask(t, 10, 5)
			require.NoError(t, writes.Reserve(ctx, &old))
			oldSent, err := writes.AcquireSend(ctx, &old)
			require.NoError(t, err)
			_, err = writes.RecordSubmission(ctx, oldSent, "old-request", "already-known")
			require.NoError(t, err)
			task := old
			task.TaskID = videoRelayFixtureID()
			require.NoError(t, writes.Reserve(ctx, &task))
			_, err = reads.AcquireSend(ctx, &task)
			require.NoError(t, err)
			ticket, claimed, err := reads.ClaimLookup(ctx, task.TaskID)
			require.NoError(t, err)
			require.True(t, claimed)
			require.NotNil(t, ticket)
			logs := []service.VideoRelayChargeRecord{{Path: "/v1/videos", RequestID: "old-request", TaskID: "already-known", Model: old.UpstreamModel}}
			if fixture.path != "" {
				candidate := service.VideoRelayChargeRecord{RequestID: "new-request", TaskID: "new-task", Path: fixture.path, Model: task.UpstreamModel}
				logs = append(logs, candidate, candidate)
			}
			confirmed, err := writes.ConfirmLookup(ctx, ticket, "/tenant/v1/videos", append(logs, fixture.extra...))
			require.NoError(t, err)
			if fixture.accept {
				require.Equal(t, "new-task", confirmed.UpstreamTaskID)
			} else {
				require.Empty(t, confirmed.UpstreamTaskID)
			}
			require.Equal(t, service.VideoRelayHeld, confirmed.BillingState)
			assertVideoRelayFunds(t, db, task, 0, 10, 0)
		})
	}
}

func TestVideoRelayRepoSettlementPersistsExactlyOneUsageRecord(t *testing.T) {
	ctx := context.Background()
	db2 := videoRelayIndependentDB(t)
	repo1, repo2 := NewVideoRelayRepository(integrationDB), NewVideoRelayRepository(db2)
	for _, hold := range []float64{5, 0} {
		task := newVideoRelayTask(t, 10, hold)
		require.NoError(t, repo1.Reserve(ctx, &task))
		sent, err := repo1.AcquireSend(ctx, &task)
		require.NoError(t, err)
		accepted, err := repo1.RecordSubmission(ctx, sent, "fixture-request", "provider-video")
		require.NoError(t, err)
		require.Equal(t, service.VideoRelayStatusQueued, accepted.Status)
		// An unrelated balance charge happened after the hold.
		_, err = db2.ExecContext(ctx, "UPDATE users SET balance=-2 WHERE id=$1", task.UserID)
		require.NoError(t, err)
		change := service.VideoRelaySettlement{
			Completed: true, AccountType: service.AccountTypeAPIKey,
			Billing: service.UsageBillingCommand{APIKeyQuotaCost: hold, APIKeyRateLimitCost: hold, AccountQuotaCost: hold},
		}
		// A held row may never be reported as charged from a preexisting dedup row.
		cmd := change.Billing
		cmd.RequestID, cmd.UserID, cmd.APIKeyID, cmd.AccountID = task.UsageRequestID(), task.UserID, task.APIKeyID, task.AccountID
		cmd.AccountType, cmd.Model, cmd.BillingType = change.AccountType, task.Model, service.BillingTypeBalance
		cmd.Normalize()
		_, err = db2.ExecContext(ctx, `INSERT INTO usage_billing_dedup (request_id, api_key_id, request_fingerprint) VALUES ($1,$2,$3)`, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint)
		require.NoError(t, err)
		conflict, _, dedupErr := repo1.Settle(ctx, accepted, change)
		require.ErrorContains(t, dedupErr, "still held with an existing usage billing key")
		require.Equal(t, service.VideoRelayHeld, conflict.BillingState)
		require.False(t, conflict.CanDownload())
		_, err = db2.ExecContext(ctx, `DELETE FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2`, cmd.RequestID, cmd.APIKeyID)
		require.NoError(t, err)
		assertVideoRelayFunds(t, db2, task, -2, hold, 0)

		_, err = db2.ExecContext(ctx, "ALTER TABLE usage_logs ADD CONSTRAINT video_relay_test_usage_insert CHECK (request_id <> "+pq.QuoteLiteral(task.UsageRequestID())+")")
		require.NoError(t, err)
		_, _, insertError := repo1.Settle(ctx, accepted, change)
		_, err = db2.ExecContext(ctx, "ALTER TABLE usage_logs DROP CONSTRAINT video_relay_test_usage_insert")
		require.NoError(t, err)
		require.Error(t, insertError)
		assertVideoRelayFunds(t, db2, task, -2, hold, 0)

		// A failure after the usage insert must roll back it and every counter.
		_, err = db2.ExecContext(ctx, "ALTER TABLE video_relay_tasks ADD CONSTRAINT video_relay_test_rollback CHECK (task_id <> "+pq.QuoteLiteral(task.TaskID)+" OR billing_state <> 'charged')")
		require.NoError(t, err)
		_, _, settleError := repo1.Settle(ctx, accepted, change)
		_, err = db2.ExecContext(ctx, "ALTER TABLE video_relay_tasks DROP CONSTRAINT video_relay_test_rollback")
		require.NoError(t, err)
		require.Error(t, settleError)
		uncommitted, err := repo2.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
		require.NoError(t, err)
		require.Equal(t, service.VideoRelayHeld, uncommitted.BillingState)
		require.Equal(t, service.VideoRelayStatusQueued, uncommitted.Status)
		assertVideoRelayFunds(t, db2, task, -2, hold, 0)

		taskID := pq.QuoteLiteral(task.TaskID)
		_, err = db2.ExecContext(ctx, fmt.Sprintf(`
			CREATE FUNCTION video_relay_fixture_skip_terminal_update() RETURNS trigger AS $$
			BEGIN RETURN NULL; END; $$ LANGUAGE plpgsql;
			CREATE TRIGGER video_relay_fixture_skip_terminal_update
			BEFORE UPDATE ON video_relay_tasks FOR EACH ROW
			WHEN (NEW.task_id = %s AND NEW.billing_state = 'charged')
			EXECUTE FUNCTION video_relay_fixture_skip_terminal_update()`, taskID))
		require.NoError(t, err)
		// A real PostgreSQL trigger can suppress a row without aborting the
		// transaction. The repository must still roll back the partial writes.
		_, _, missingTerminalError := repo1.Settle(ctx, accepted, change)
		_, err = db2.ExecContext(ctx, `
			DROP TRIGGER video_relay_fixture_skip_terminal_update ON video_relay_tasks;
			DROP FUNCTION video_relay_fixture_skip_terminal_update()`)
		require.NoError(t, err)
		var afterSuppression float64
		require.NoError(t, db2.QueryRowContext(ctx, "SELECT frozen_balance FROM users WHERE id=$1", task.UserID).Scan(&afterSuppression))
		require.InDelta(t, hold, afterSuppression, 1e-8, "missing terminal update must roll back the earlier hold debit")
		assertVideoRelayFunds(t, db2, task, -2, hold, 0)
		require.ErrorIs(t, missingTerminalError, service.ErrVideoRelayNotFound)
		uncommitted, err = repo2.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
		require.NoError(t, err)
		require.Equal(t, service.VideoRelayHeld, uncommitted.BillingState)
		require.False(t, uncommitted.CanDownload())

		done := make(chan error, 2)
		for _, repo := range []service.VideoRelayRepository{repo1, repo2} {
			go func() {
				_, _, err := repo.Settle(ctx, accepted, change)
				done <- err
			}()
		}
		require.NoError(t, <-done)
		require.NoError(t, <-done)
		finished, err := repo2.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
		require.NoError(t, err)
		require.Equal(t, service.VideoRelayCharged, finished.BillingState)
		require.Equal(t, service.VideoRelayStatusCompleted, finished.Status)
		assertVideoRelayFunds(t, db2, task, -2, 0, 1)

		var model, requestedModel, upstreamModel, mode, resolution string
		var seconds, videos int
		var actualCost, totalCost, rate, quotaUsed, windowUsed float64
		require.NoError(t, db2.QueryRowContext(ctx, `
			SELECT model, requested_model, upstream_model, billing_mode, video_resolution,
				video_duration_seconds, video_count, actual_cost, total_cost, rate_multiplier
			FROM usage_logs WHERE request_id=$1 AND api_key_id=$2`, task.UsageRequestID(), task.APIKeyID).
			Scan(&model, &requestedModel, &upstreamModel, &mode, &resolution, &seconds, &videos, &actualCost, &totalCost, &rate))
		require.Equal(t, task.Model, model)
		require.Equal(t, task.Model, requestedModel)
		require.Equal(t, task.UpstreamModel, upstreamModel)
		require.Equal(t, string(service.BillingModeVideo), mode)
		require.Equal(t, task.Resolution, resolution)
		require.Equal(t, task.Seconds, seconds)
		require.Equal(t, 1, videos)
		require.InDelta(t, task.HoldAmount, actualCost, 0.00000001)
		require.InDelta(t, task.TotalCost, totalCost, 0.00000001)
		require.InDelta(t, task.RateMultiplier, rate, 0.00000001)
		require.NoError(t, db2.QueryRowContext(ctx, "SELECT quota_used, usage_5h FROM api_keys WHERE id=$1", task.APIKeyID).Scan(&quotaUsed, &windowUsed))
		require.InDelta(t, hold, quotaUsed, 0.00000001)
		require.InDelta(t, hold, windowUsed, 0.00000001)
		var accountQuota float64
		require.NoError(t, db2.QueryRowContext(ctx, `SELECT COALESCE((extra->>'quota_used')::numeric, 0) FROM accounts WHERE id=$1`, task.AccountID).Scan(&accountQuota))
		require.InDelta(t, hold, accountQuota, 0.00000001)

		late, _, err := repo1.Settle(ctx, accepted, service.VideoRelaySettlement{FailureReason: service.VideoRelayUpstreamFailed})
		require.NoError(t, err)
		require.Equal(t, service.VideoRelayCharged, late.BillingState)
		assertVideoRelayFunds(t, db2, task, -2, 0, 1)
	}
}

func TestVideoRelayRepoUpstreamFailureRefundsOriginalHoldOnce(t *testing.T) {
	ctx := context.Background()
	task := newVideoRelayTask(t, 10, 5)
	repo := NewVideoRelayRepository(integrationDB)
	require.NoError(t, repo.Reserve(ctx, &task))
	sent, err := repo.AcquireSend(ctx, &task)
	require.NoError(t, err)
	accepted, err := repo.RecordSubmission(ctx, sent, "fixture-request", "provider-video")
	require.NoError(t, err)
	first, _, err := repo.Settle(ctx, accepted, service.VideoRelaySettlement{FailureReason: service.VideoRelayUpstreamFailed})
	require.NoError(t, err)
	require.Equal(t, service.VideoRelayRefunded, first.BillingState)
	require.Equal(t, service.VideoRelayStatusFailed, first.Status)
	require.NotNil(t, first.SettledAt)
	db2 := videoRelayIndependentDB(t)
	reconnected := NewVideoRelayRepository(db2)
	assertVideoRelayFunds(t, db2, task, 10, 0, 0)
	other := task
	other.TaskID = videoRelayFixtureID()
	require.NoError(t, reconnected.Reserve(ctx, &other))
	late, _, err := reconnected.Settle(ctx, accepted, service.VideoRelaySettlement{Completed: true})
	current, readErr := reconnected.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
	require.NoError(t, readErr)
	require.Equal(t, service.VideoRelayRefunded, current.BillingState,
		"late completion must leave the original refund even when another order has frozen funds")
	require.False(t, current.CanDownload())
	assertVideoRelayFunds(t, db2, task, 5, 5, 0)
	require.NoError(t, err)
	require.Equal(t, first.SettledAt.Truncate(time.Microsecond), late.SettledAt.Truncate(time.Microsecond))
	require.Equal(t, service.VideoRelayRefunded, late.BillingState)
}

func TestVideoRelayRepoRefundsDeletedUserEvenForFreeSettlement(t *testing.T) {
	ctx := context.Background()
	repo := NewVideoRelayRepository(integrationDB)
	db2 := videoRelayIndependentDB(t)
	for _, hold := range []float64{5, 0} {
		task := newVideoRelayTask(t, 10, hold)
		require.NoError(t, repo.Reserve(ctx, &task))
		sent, err := repo.AcquireSend(ctx, &task)
		require.NoError(t, err)
		accepted, err := repo.RecordSubmission(ctx, sent, "", "provider-video")
		require.NoError(t, err)
		_, err = db2.ExecContext(ctx, "UPDATE users SET deleted_at=clock_timestamp() WHERE id=$1", task.UserID)
		require.NoError(t, err)
		finished, _, err := repo.Settle(ctx, accepted, service.VideoRelaySettlement{Completed: true, AccountType: service.AccountTypeAPIKey})
		require.NoError(t, err, "a deleted owner must get the original refund on their own row")
		require.Equal(t, service.VideoRelayRefunded, finished.BillingState)
		require.Equal(t, "user_deleted", finished.FailureReason)
		current, err := repo.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
		require.NoError(t, err)
		require.Equal(t, service.VideoRelayRefunded, current.BillingState)
		require.False(t, current.CanDownload())
		var deleted bool
		require.NoError(t, db2.QueryRowContext(ctx, "SELECT deleted_at IS NOT NULL FROM users WHERE id=$1", task.UserID).Scan(&deleted))
		require.True(t, deleted, "refund must preserve the owner's deletion")
		assertVideoRelayFunds(t, db2, task, 10, 0, 0)
	}
}

type videoRelayGatewayFaults struct {
	service.VideoRelayRepository
	loseRefundResult         bool
	failSend, loseSendResult bool
	failLookupConfirmation   bool
}

func (r *videoRelayGatewayFaults) Settle(ctx context.Context, task *service.VideoRelayTask, change service.VideoRelaySettlement) (*service.VideoRelayTask, *service.UsageBillingApplyResult, error) {
	updated, result, err := r.VideoRelayRepository.Settle(ctx, task, change)
	if err == nil && r.loseRefundResult && change.FailureReason == service.VideoRelayUpstreamRejected {
		return nil, nil, errors.New("fixture lost the committed settlement response")
	}
	return updated, result, err
}

func (r *videoRelayGatewayFaults) AcquireSend(ctx context.Context, task *service.VideoRelayTask) (*service.VideoRelayTask, error) {
	if r.failSend {
		return nil, errors.New("fixture could not start the send transaction")
	}
	sent, err := r.VideoRelayRepository.AcquireSend(ctx, task)
	if err == nil && r.loseSendResult {
		return nil, errors.New("fixture lost the committed send transaction response")
	}
	return sent, err
}

func (r *videoRelayGatewayFaults) ConfirmLookup(ctx context.Context, ticket *service.VideoRelayTask, path string, logs []service.VideoRelayChargeRecord) (*service.VideoRelayTask, error) {
	if r.failLookupConfirmation {
		return nil, errors.New("fixture could not begin the confirmation transaction")
	}
	return r.VideoRelayRepository.ConfirmLookup(ctx, ticket, path, logs)
}

func TestVideoRelayRepoZeroRateSavedThroughGroupAPIAndChargedThroughGateway(t *testing.T) {
	ctx := context.Background()
	fixture := newVideoRelayTask(t, 5, 0)
	var posts atomic.Int32
	var reject atomic.Bool
	var logs atomic.Int32
	var includeEncodedLogPath atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodGet && req.URL.EscapedPath() == "/tenant%2Fother/api/log/token" {
			logPath := "/tenant/other/v1/videos"
			if includeEncodedLogPath.Load() {
				logPath = "/tenant%2Fother/v1/videos"
			}
			_, _ = io.WriteString(w, `{"success":true,"data":[{
				"type":2,"request_id":"encoded-route-request","model_name":"fixture-model-480p",
				"other":{"task_id":"encoded-route-video","request_path":"`+logPath+`"}
			}]}`)
			return
		}
		if req.Method == http.MethodGet && req.URL.EscapedPath() == "/tenant%2Fother/v1/videos/encoded-route-video" {
			_, _ = io.WriteString(w, `{"id":"encoded-route-video","status":"running","progress":49}`)
			return
		}
		if req.Method == http.MethodPost && req.URL.EscapedPath() == "/tenant%2Fother/v1/videos" {
			posts.Add(1)
			w.Header().Set("X-Oneapi-Request-Id", "encoded-route-request")
			_, _ = io.WriteString(w, `{"status":"queued"}`)
			return
		}
		if req.Method == http.MethodGet && req.URL.Path == "/api/log/token" {
			logs.Add(1)
			_, _ = io.WriteString(w, `{"success":true,"data":[{
				"type":2,"request_id":"late-log","model_name":"fixture-model-480p",
				"other":{"task_id":"late-fixture-task","request_path":"/v1/videos"}
			}]}`)
			return
		}
		if req.Method != http.MethodPost || req.URL.Path != "/v1/videos" {
			http.NotFound(w, req)
			return
		}
		posts.Add(1)
		w.Header().Set("X-Oneapi-Request-Id", "simulator-request")
		if reject.Load() {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"provider-only rejection"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"simulator-task","status":"completed","delivery_status":"ready"}`)
	}))
	t.Cleanup(upstream.Close)
	client := testEntClient(t)
	accountRepo := NewAccountRepository(client, integrationDB, nil)
	account, err := accountRepo.GetByID(ctx, fixture.AccountID)
	require.NoError(t, err)
	account.Credentials = map[string]any{
		"base_url": upstream.URL, "api_key": "simulator-bearer", "openai_capabilities": []string{"videos"},
		"openai_videos": map[string]any{"resolutions": []string{"480p"}, "min_seconds": 5, "default_seconds": 5, "max_seconds": 30},
		"model_mapping": map[string]any{fixture.Model: fixture.UpstreamModel},
	}
	account.Status, account.Schedulable = service.StatusActive, true
	accountRate := 2.0
	account.RateMultiplier = &accountRate
	require.NoError(t, accountRepo.Update(ctx, account))
	require.NoError(t, accountRepo.BindGroups(ctx, account.ID, []int64{fixture.GroupID}))

	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Security.URLAllowlist.AllowPrivateHosts = true
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.MaxBodySize = 64 * 1024
	cfg.Gateway.TextMaxBodySize = 64 * 1024
	userRepo := NewUserRepository(client, integrationDB)
	groupRepo := NewGroupRepository(client, integrationDB)
	apiKeyRepo := NewAPIKeyRepository(client, integrationDB)
	key, err := apiKeyRepo.GetByID(ctx, fixture.APIKeyID)
	require.NoError(t, err)
	adminService := service.NewAdminService(cfg, userRepo, NewAdminGroupRepository(client, integrationDB), nil, nil, apiKeyRepo,
		nil, nil, nil, nil, nil, nil, nil, client, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	groupHandler := admin.NewGroupHandlerWithConfig(adminService, nil, nil, cfg)
	groupRoutes := gin.New()
	groupRoutes.PUT("/api/v1/admin/groups/:id", groupHandler.Update)
	setPrice := httptest.NewRequest(http.MethodPut, "/api/v1/admin/groups/"+strconv.FormatInt(fixture.GroupID, 10), strings.NewReader(`{
		"allow_image_generation":true,"rate_multiplier":1,"video_rate_independent":true,"video_rate_multiplier":0,
		"model_pricing":[{"platform":"openai","billing_mode":"video","models":["video-public"],"per_request_price":0.1}]
	}`))
	setPrice.Header.Set("Content-Type", "application/json")
	updated := httptest.NewRecorder()
	groupRoutes.ServeHTTP(updated, setPrice)
	require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
	group, err := groupRepo.GetByID(ctx, fixture.GroupID)
	require.NoError(t, err)
	require.True(t, group.VideoRateIndependent)
	require.Zero(t, group.VideoRateMultiplier)

	billing := service.NewBillingService(cfg, nil)
	concurrency := service.NewConcurrencyService(NewConcurrencyCache(testRedis(t), 1, 10))
	billingCacheStore := NewBillingCache(testRedis(t))
	require.NoError(t, billingCacheStore.SetUserBalance(ctx, fixture.UserID, 5))
	cache := service.NewBillingCacheService(billingCacheStore, userRepo, nil, apiKeyRepo, nil, nil, cfg, nil)
	t.Cleanup(cache.Stop)
	keys := service.NewAPIKeyService(apiKeyRepo, userRepo, groupRepo, nil, nil, nil, cfg)
	openAI := service.NewOpenAIGatewayService(accountRepo, nil, nil, userRepo, nil, nil, nil, cfg, nil, concurrency,
		billing, nil, cache, NewHTTPUpstream(cfg), nil, nil, nil, service.NewModelPricingResolver(nil, billing), nil, nil, nil, nil)
	gatewayHandler := handler.NewOpenAIGatewayHandler(openAI, concurrency, cache, keys, nil, nil, nil, nil, cfg)
	repo := &videoRelayGatewayFaults{VideoRelayRepository: NewVideoRelayRepository(integrationDB)}
	relay := service.NewVideoRelayService(repo, openAI, keys)
	videoRoutes := gin.New()
	routes.RegisterGatewayRoutes(videoRoutes, &handler.Handlers{OpenAIGateway: gatewayHandler, VideoRelay: handler.NewVideoRelayHandler(relay, gatewayHandler)},
		middleware.NewAPIKeyAuthMiddleware(keys, nil, cfg), keys, nil, nil, nil, nil, cfg)
	submit := func(seconds string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"video-public","prompt":"Waves","seconds":`+seconds+`,"resolution":"480p"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+key.Key)
		response := httptest.NewRecorder()
		videoRoutes.ServeHTTP(response, request)
		return response
	}
	outside := submit("31")
	require.Zero(t, posts.Load(), "an unsupported video duration must make zero collection POSTs")
	require.Equal(t, http.StatusBadRequest, outside.Code, outside.Body.String())
	require.Contains(t, outside.Body.String(), "No available video route supports these seconds and resolution")
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM video_relay_tasks WHERE user_id=$1`, fixture.UserID).Scan(&count))
	require.Zero(t, count)
	assertVideoRelayFunds(t, integrationDB, fixture, 5, 0, 0)

	accepted := submit("30")
	require.Equal(t, http.StatusOK, accepted.Code, accepted.Body.String())
	var response struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(accepted.Body.Bytes(), &response))
	require.Equal(t, service.VideoRelayStatusCompleted, response.Status)
	require.True(t, service.ValidVideoRelayTaskID(response.ID))
	require.Equal(t, int32(1), posts.Load())
	fixture.TaskID = response.ID
	assertVideoRelayFunds(t, integrationDB, fixture, 5, 0, 1)
	var charged, price float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT actual_cost,total_cost FROM usage_logs WHERE request_id=$1 AND api_key_id=$2`, fixture.UsageRequestID(), fixture.APIKeyID).Scan(&charged, &price))
	require.Zero(t, charged)
	require.InDelta(t, 3, price, 0.00000001)
	var accountQuota, keyQuota, keyWindow float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COALESCE((extra->>'quota_used')::numeric,0) FROM accounts WHERE id=$1`, fixture.AccountID).Scan(&accountQuota))
	require.InDelta(t, 6, accountQuota, 0.00000001, "free customer video still uses the positive provider cost times the account multiplier")
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT quota_used,usage_5h FROM api_keys WHERE id=$1", fixture.APIKeyID).Scan(&keyQuota, &keyWindow))
	require.Zero(t, keyQuota)
	require.Zero(t, keyWindow)
	cached, err := billingCacheStore.GetUserBalance(ctx, fixture.UserID)
	require.NoError(t, err, "a real zero-fee order must leave the cached wallet intact")
	require.Equal(t, 5.0, cached)

	setPaidRate := httptest.NewRequest(http.MethodPut, "/api/v1/admin/groups/"+strconv.FormatInt(fixture.GroupID, 10),
		strings.NewReader(`{"video_rate_multiplier":1}`))
	setPaidRate.Header.Set("Content-Type", "application/json")
	paidRate := httptest.NewRecorder()
	groupRoutes.ServeHTTP(paidRate, setPaidRate)
	require.Equal(t, http.StatusOK, paidRate.Code, paidRate.Body.String())
	keys.InvalidateAuthCacheByUserID(ctx, fixture.UserID)

	t.Run("recheck the real database when the refund response is lost", func(t *testing.T) {
		reject.Store(true)
		repo.loseRefundResult = true
		t.Cleanup(func() {
			reject.Store(false)
			repo.loseRefundResult = false
		})
		rejected := submit("30")
		require.Equal(t, int32(2), posts.Load())
		secondDB := videoRelayIndependentDB(t)
		refunded := fixture
		var state, reason string
		require.NoError(t, secondDB.QueryRowContext(ctx, `
			SELECT task_id,hold_amount,billing_state,failure_reason FROM video_relay_tasks
			WHERE user_id=$1 AND task_id<>$2`, fixture.UserID, fixture.TaskID).
			Scan(&refunded.TaskID, &refunded.HoldAmount, &state, &reason))
		require.InDelta(t, 3, refunded.HoldAmount, 1e-8, "the rejected order must have originally frozen a nonzero amount")
		assertVideoRelayFunds(t, secondDB, refunded, 5, 0, 0)
		require.Equal(t, service.VideoRelayRefunded, state)
		require.Equal(t, service.VideoRelayUpstreamRejected, reason)
		require.Contains(t, rejected.Body.String(), "The frozen amount was returned.", "explicit rejection must report the persisted refund")
		require.Equal(t, http.StatusBadGateway, rejected.Code, rejected.Body.String())
		require.NotContains(t, rejected.Body.String(), "provider-only rejection")
	})

	for _, fault := range []struct {
		name      string
		committed bool
	}{
		{"send authorization fails before commit", false},
		{"send authorization commits but response is lost", true},
	} {
		t.Run(fault.name, func(t *testing.T) {
			repo.failSend, repo.loseSendResult = !fault.committed, fault.committed
			t.Cleanup(func() { repo.failSend, repo.loseSendResult = false, false })
			before := posts.Load()
			response := submit("30")
			require.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
			require.Contains(t, response.Body.String(), "The frozen amount was returned.")
			require.Equal(t, before, posts.Load(), "the HTTP client must never receive the POST")
			secondDB := videoRelayIndependentDB(t)
			var id string
			var sent bool
			var hold float64
			require.NoError(t, secondDB.QueryRowContext(ctx, `
				SELECT task_id, sent_at IS NOT NULL, hold_amount FROM video_relay_tasks
				WHERE user_id=$1 AND failure_reason='not_sent' AND billing_state='refunded'
				ORDER BY created_at DESC LIMIT 1`, fixture.UserID).Scan(&id, &sent, &hold))
			require.Equal(t, fault.committed, sent)
			require.InDelta(t, 3.0, hold, 1e-8)
			refunded := fixture
			refunded.TaskID = id
			assertVideoRelayFunds(t, secondDB, refunded, 5, 0, 0)
		})
	}

	t.Run("deadline refund continues after an uncommitted confirmation failure", func(t *testing.T) {
		current, err := repo.Get(ctx, fixture.TaskID, fixture.UserID, fixture.APIKeyID)
		require.NoError(t, err)
		task := *current
		task.TaskID, task.HoldAmount = videoRelayFixtureID(), 3
		require.NoError(t, repo.Reserve(ctx, &task))
		sent, err := repo.AcquireSend(ctx, &task)
		require.NoError(t, err)
		_, err = repo.RecordSubmission(ctx, sent, "late-log", "")
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(ctx, `
			UPDATE video_relay_tasks SET sent_at=clock_timestamp()-interval '31 minutes' WHERE task_id=$1`, task.TaskID)
		require.NoError(t, err)
		repo.failLookupConfirmation = true
		t.Cleanup(func() { repo.failLookupConfirmation = false })
		request := httptest.NewRequest(http.MethodGet, "/v1/videos/"+task.TaskID, nil)
		request.Header.Set("Authorization", "Bearer "+key.Key)
		response := httptest.NewRecorder()
		videoRoutes.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		require.Equal(t, int32(1), logs.Load())
		secondDB := videoRelayIndependentDB(t)
		reloaded, err := NewVideoRelayRepository(secondDB).Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
		require.NoError(t, err)
		require.Equal(t, service.VideoRelayRefunded, reloaded.BillingState)
		require.Equal(t, service.VideoRelayUnconfirmed, reloaded.FailureReason)
		require.Empty(t, reloaded.UpstreamTaskID)
		require.Contains(t, response.Body.String(), "The frozen amount was returned.")
		assertVideoRelayFunds(t, secondDB, task, 5, 0, 0)
	})

	t.Run("confirm against the path actually sent on the wire", func(t *testing.T) {
		account.Credentials["base_url"] = upstream.URL + "/tenant%2Fother"
		require.NoError(t, accountRepo.Update(ctx, account))
		before := posts.Load()
		started := submit("30")
		require.Equal(t, http.StatusOK, started.Code, started.Body.String())
		require.Equal(t, before+1, posts.Load())
		var submitted struct {
			ID string `json:"id"`
		}
		require.NoError(t, json.Unmarshal(started.Body.Bytes(), &submitted))
		require.True(t, service.ValidVideoRelayTaskID(submitted.ID))
		secondDB := videoRelayIndependentDB(t)
		reads := NewVideoRelayRepository(secondDB)
		lookup := func() *httptest.ResponseRecorder {
			request := httptest.NewRequest(http.MethodGet, "/v1/videos/"+submitted.ID, nil)
			request.Header.Set("Authorization", "Bearer "+key.Key)
			response := httptest.NewRecorder()
			videoRoutes.ServeHTTP(response, request)
			return response
		}
		wrong := lookup()
		require.Equal(t, http.StatusOK, wrong.Code, wrong.Body.String())
		task, err := reads.Get(ctx, submitted.ID, fixture.UserID, fixture.APIKeyID)
		require.NoError(t, err)
		require.Empty(t, task.UpstreamTaskID, "a decoded slash can identify another upstream route")
		require.Equal(t, service.VideoRelayHeld, task.BillingState)
		assertVideoRelayFunds(t, secondDB, *task, 2, 3, 0)

		includeEncodedLogPath.Store(true)
		_, err = integrationDB.ExecContext(ctx, `
			UPDATE video_relay_tasks SET last_lookup_at=clock_timestamp()-interval '16 seconds' WHERE task_id=$1`, submitted.ID)
		require.NoError(t, err)
		found := lookup()
		require.Equal(t, http.StatusOK, found.Code, found.Body.String())
		task, err = reads.Get(ctx, submitted.ID, fixture.UserID, fixture.APIKeyID)
		require.NoError(t, err)
		require.Equal(t, "encoded-route-video", task.UpstreamTaskID)
		require.Equal(t, service.VideoRelayHeld, task.BillingState)
		require.Equal(t, service.VideoRelayStatusInProgress, task.Status)
		require.Equal(t, 49, task.Progress)
		assertVideoRelayFunds(t, secondDB, *task, 2, 3, 0)
	})
}
