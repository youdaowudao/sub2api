//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func fixtureVideoRelayFingerprint(parts ...string) string {
	digest := sha256.New()
	for _, part := range parts {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(part)))
		_, _ = digest.Write(length[:])
		_, _ = io.WriteString(digest, part)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

type trackingVideoRelayFixtureOptions struct {
	upstream     service.HTTPUpstream
	concurrency  *service.ConcurrencyService
	billingCache *service.BillingCacheService
	wrapRepo     func(service.VideoRelayRepository) service.VideoRelayRepository
}

func newTrackingVideoRelayFixtureGateway(t *testing.T, accounts service.AccountRepository, options trackingVideoRelayFixtureOptions) *service.OpenAIGatewayService {
	t.Helper()
	cfg := &config.Config{RunMode: config.RunModeStandard}
	cfg.Security.URLAllowlist.AllowPrivateHosts = true
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	concurrency := options.concurrency
	if concurrency == nil {
		concurrency = service.NewConcurrencyService(NewConcurrencyCache(testRedis(t), 1, 10))
	}
	upstream := options.upstream
	if upstream == nil {
		upstream = NewHTTPUpstream(cfg)
	}
	billing := service.NewBillingService(cfg, nil)
	return service.NewOpenAIGatewayService(accounts, nil, nil, nil, nil, nil, nil, cfg, nil, concurrency,
		billing, nil, options.billingCache, upstream, nil, nil, nil, service.NewModelPricingResolver(nil, billing), nil, nil, nil, nil)
}

func newTrackingVideoRelayFixture(t *testing.T, upstreamURL string, age time.Duration, options ...trackingVideoRelayFixtureOptions) (service.VideoRelayTask, service.VideoRelayRepository, *service.VideoRelayTracker) {
	t.Helper()
	ctx := context.Background()
	task := newVideoRelayTask(t, 10, 5)
	client := testEntClient(t)
	accounts := NewAccountRepository(client, integrationDB, nil)
	account, err := accounts.GetByID(ctx, task.AccountID)
	require.NoError(t, err)
	const bearer = "local-fixture-bearer"
	account.Credentials = map[string]any{"base_url": upstreamURL, "api_key": bearer}
	require.NoError(t, accounts.Update(ctx, account))
	task.SiteFingerprint = fixtureVideoRelayFingerprint(upstreamURL + "/v1/videos")
	task.CredentialFingerprint = fixtureVideoRelayFingerprint(bearer)
	task.RouteFingerprint = fixtureVideoRelayFingerprint(task.SiteFingerprint, task.CredentialFingerprint)
	var fixture trackingVideoRelayFixtureOptions
	if len(options) != 0 {
		fixture = options[0]
	}
	gateway := newTrackingVideoRelayFixtureGateway(t, accounts, fixture)
	repo := NewVideoRelayRepository(integrationDB)
	require.NoError(t, repo.Reserve(ctx, &task))
	sent, err := repo.AcquireSend(ctx, &task)
	require.NoError(t, err)
	_, err = repo.RecordSubmission(ctx, sent, "tracking-fixture-request", "tracking-fixture-video")
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE video_relay_tasks SET created_at=clock_timestamp()-make_interval(secs => $2) WHERE task_id=$1`, task.TaskID, age.Seconds())
	require.NoError(t, err)
	makeVideoRelayTaskDue(t, integrationDB, task.TaskID)
	if fixture.wrapRepo != nil {
		repo = fixture.wrapRepo(repo)
	}
	return task, repo, service.NewVideoRelayTracker(service.NewVideoRelayService(repo, gateway, nil))
}

func TestVideoRelayRepoTrackingExpiredBodyTimeout(t *testing.T) {
	for _, test := range []struct {
		name               string
		response           string
		cancel, failInsert bool
		want               string
		balance, frozen    float64
		usage              int
	}{
		{"upstream body timeout refunds original amount", "", false, false, service.VideoRelayTrackingExpired, 10, 0, 0},
		{"shutdown during body read preserves the hold", "", true, false, "", 5, 5, 0},
		{"trusted completion is charged even at the deadline", `{"id":"tracking-fixture-video","status":"completed","delivery_status":"ready"}`, false, false, "", 5, 0, 1},
		{"trusted failure returns the amount with its original cause", `{"id":"tracking-fixture-video","status":"failed"}`, false, false, service.VideoRelayUpstreamFailed, 10, 0, 0},
		{"trusted completion with a write failure preserves the hold", `{"id":"tracking-fixture-video","status":"completed","delivery_status":"ready"}`, false, true, "", 5, 5, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			var reads atomic.Int32
			started := make(chan struct{})
			aborted := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/v1/videos/tracking-fixture-video" {
					http.NotFound(w, r)
					return
				}
				if reads.Add(1) != 1 {
					http.Error(w, "duplicate tracking request", http.StatusConflict)
					return
				}
				w.WriteHeader(http.StatusOK)
				if test.response != "" {
					_, _ = io.WriteString(w, test.response)
					return
				}
				w.(http.Flusher).Flush()
				close(started)
				<-r.Context().Done()
				close(aborted)
			}))
			t.Cleanup(srv.Close)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			task, repo, tracker := newTrackingVideoRelayFixture(t, srv.URL, 5*time.Hour+time.Minute)
			if test.failInsert {
				taskID := pq.QuoteLiteral(task.UsageRequestID())
				_, err := integrationDB.ExecContext(ctx, fmt.Sprintf(`
					CREATE FUNCTION video_tracking_fixture_fail_usage() RETURNS trigger AS $$
					BEGIN RAISE EXCEPTION 'test-only tracking usage insert rejected'; END; $$ LANGUAGE plpgsql;
					CREATE TRIGGER video_tracking_fixture_fail_usage BEFORE INSERT ON usage_logs
					FOR EACH ROW WHEN (NEW.request_id = %s) EXECUTE FUNCTION video_tracking_fixture_fail_usage()
				`, taskID))
				require.NoError(t, err)
				t.Cleanup(func() {
					_, err := integrationDB.Exec(
						"DROP TRIGGER video_tracking_fixture_fail_usage ON usage_logs; DROP FUNCTION video_tracking_fixture_fail_usage()")
					require.NoError(t, err)
				})
			}
			claim, err := repo.ClaimDue(ctx)
			require.NoError(t, err)
			require.NotNil(t, claim)
			require.Equal(t, task.TaskID, claim.TaskID)
			require.WithinDuration(t, time.Now(), *claim.SentAt, time.Minute, "only creation is old enough for the final check")
			done := make(chan struct{})
			start := time.Now()
			go func() {
				tracker.ProcessClaim(ctx, claim)
				close(done)
			}()
			if test.response == "" {
				select {
				case <-started:
				case <-time.After(3 * time.Second):
					t.Fatal("local server did not flush its response headers")
				}
				if test.cancel {
					cancel()
				}
				select {
				case <-aborted:
				case <-time.After(22 * time.Second):
					t.Fatal("client did not cancel the blocked upstream body")
				}
				if !test.cancel {
					require.GreaterOrEqual(t, time.Since(start), 14*time.Second)
				}
			}
			select {
			case <-done:
			case <-time.After(8 * time.Second):
				t.Fatal("tracking item did not exit")
			}
			got, err := repo.Get(context.Background(), task.TaskID, task.UserID, task.APIKeyID)
			require.NoError(t, err)
			require.Equal(t, test.want, got.FailureReason)
			assertVideoRelayFunds(t, integrationDB, task, test.balance, test.frozen, test.usage)
			require.EqualValues(t, 1, reads.Load())
			if test.want != "" {
				require.Equal(t, service.VideoRelayRefunded, got.BillingState)
				require.False(t, got.CanDownload())
				require.Nil(t, got.NextCheckAt)
			}
			if test.cancel || test.failInsert {
				require.Equal(t, service.VideoRelayHeld, got.BillingState)
				require.NotNil(t, got.NextCheckAt)
			}
		})
	}
}

func TestVideoRelayRepoKnownDeadlineRecoversThroughPersistedSchedule(t *testing.T) {
	for _, scheduled := range []bool{true, false} {
		name := "canceled request keeps the two-minute lease"
		if scheduled {
			name = "failed settlement schedules the five-minute retry"
		}
		t.Run(name, func(t *testing.T) {
			var requests atomic.Int32
			arrived := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/v1/videos/tracking-fixture-video" {
					http.NotFound(w, r)
					return
				}
				if requests.Add(1) == 1 && !scheduled {
					close(arrived)
					<-r.Context().Done()
					return
				}
				_, _ = io.WriteString(w, `{"id":"tracking-fixture-video","status":"completed","delivery_status":"ready"}`)
			}))
			t.Cleanup(srv.Close)
			ctx := context.Background()
			task, repo, tracker := newTrackingVideoRelayFixture(t, srv.URL, 5*time.Hour+time.Minute)
			claim, err := repo.ClaimDue(ctx)
			require.NoError(t, err)
			require.NotNil(t, claim)
			require.Equal(t, task.TaskID, claim.TaskID)
			require.True(t, claim.SentAt.After(claim.CreatedAt.Add(5*time.Hour)),
				"the five-hour deadline is based on creation, even when the send was recent")

			db2 := videoRelayIndependentDB(t)
			if scheduled {
				tx, err := db2.BeginTx(ctx, nil)
				require.NoError(t, err)
				defer tx.Rollback()
				var id int64
				require.NoError(t, tx.QueryRowContext(ctx,
					"SELECT id FROM users WHERE id=$1 FOR NO KEY UPDATE", task.UserID).Scan(&id))
				tracker.ProcessClaim(ctx, claim)
				require.NoError(t, tx.Rollback())
			} else {
				work, cancel := context.WithCancel(ctx)
				defer cancel()
				done := make(chan struct{})
				go func() {
					defer close(done)
					tracker.ProcessClaim(work, claim)
				}()
				select {
				case <-arrived:
				case <-time.After(5 * time.Second):
					t.Fatal("the first deadline status request never reached the provider fixture")
				}
				cancel()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("the canceled status request did not return")
				}
			}

			require.EqualValues(t, 1, requests.Load())
			assertVideoRelayFunds(t, db2, task, 5, 5, 0)
			after := NewVideoRelayRepository(db2)
			current, err := after.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
			require.NoError(t, err)
			require.Equal(t, service.VideoRelayHeld, current.BillingState)
			require.NotNil(t, current.NextCheckAt)
			if scheduled {
				var now time.Time
				require.NoError(t, db2.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&now))
				require.WithinDuration(t, now.Add(5*time.Minute), *current.NextCheckAt, 3*time.Second)
			} else {
				require.True(t, current.NextCheckAt.Equal(*claim.TrackingLease), "cancellation must leave the old lease intact")
			}
			early, err := after.ClaimDue(ctx)
			require.NoError(t, err)
			if early != nil {
				require.NotEqual(t, task.TaskID, early.TaskID, "the original task must not be claimable before its stored retry time")
			}
			require.EqualValues(t, 1, requests.Load())

			makeVideoRelayTaskDue(t, db2, task.TaskID)
			recovered, err := after.ClaimDue(ctx)
			require.NoError(t, err)
			require.NotNil(t, recovered)
			require.Equal(t, task.TaskID, recovered.TaskID)
			tracker.ProcessClaim(ctx, recovered)
			require.EqualValues(t, 2, requests.Load(), "after handover, exactly one status request may complete the task")
			assertVideoRelayFunds(t, db2, task, 5, 0, 1)
			_, _, err = repo.Settle(ctx, claim, service.VideoRelaySettlement{FailureReason: service.VideoRelayTrackingExpired})
			require.ErrorIs(t, err, service.ErrVideoRelayLeaseLost)
			current, err = after.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
			require.NoError(t, err)
			require.Equal(t, service.VideoRelayCharged, current.BillingState)
			require.Nil(t, current.NextCheckAt)
			assertVideoRelayFunds(t, db2, task, 5, 0, 1)
		})
	}
}

func TestVideoRelayRepoStatusStartedBeforeDeadlineKeepsHold(t *testing.T) {
	var calls atomic.Int32
	var firstRequestAt atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/videos/tracking-fixture-video" {
			http.NotFound(w, r)
			return
		}
		if calls.Add(1) == 1 {
			firstRequestAt.Store(time.Now().UnixNano())
			select {
			case <-time.After(2 * time.Second):
			case <-r.Context().Done():
				return
			}
		}
		_, _ = io.WriteString(w, `{"id":"tracking-fixture-video","status":"in_progress","progress":25}`)
	}))
	t.Cleanup(srv.Close)
	ctx := context.Background()
	task, repo, tracker := newTrackingVideoRelayFixture(t, srv.URL, 5*time.Hour-1200*time.Millisecond)
	first, err := repo.ClaimDue(ctx)
	require.NoError(t, err)
	require.NotNil(t, first)
	require.Equal(t, task.TaskID, first.TaskID)
	cutoff := first.CreatedAt.Add(5 * time.Hour)
	require.True(t, first.TrackingStartedAt.Before(cutoff), "the initial status request must start before the creation cutoff")
	tracker.ProcessClaim(ctx, first)
	require.EqualValues(t, 1, calls.Load())
	require.Less(t, firstRequestAt.Load(), cutoff.UnixNano(), "the provider must receive the first request before cutoff")
	current, err := repo.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
	require.NoError(t, err)
	require.Equal(t, service.VideoRelayHeld, current.BillingState)
	require.Equal(t, 25, current.Progress)
	require.NotNil(t, current.NextCheckAt)
	require.False(t, current.NextCheckAt.Before(cutoff), "the response must arrive after the cutoff")
	assertVideoRelayFunds(t, integrationDB, task, 5, 5, 0)

	var now time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&now))
	require.False(t, current.NextCheckAt.After(now), "a check that crosses the deadline must make the final check due immediately")
	// The shared integration database may have other due fixtures. This write
	// only picks the task already proven due ahead of the other candidates.
	makeVideoRelayTaskDue(t, integrationDB, task.TaskID)
	last, err := repo.ClaimDue(ctx)
	require.NoError(t, err)
	require.NotNil(t, last)
	require.Equal(t, task.TaskID, last.TaskID)
	require.False(t, last.TrackingStartedAt.Before(cutoff))
	tracker.ProcessClaim(ctx, last)
	current, err = repo.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
	require.NoError(t, err)
	require.EqualValues(t, 2, calls.Load())
	require.Equal(t, service.VideoRelayTrackingExpired, current.FailureReason)
	require.Nil(t, current.NextCheckAt)
	assertVideoRelayFunds(t, integrationDB, task, 10, 0, 0)
}

func TestVideoRelayRepoUnknownTrackingUsesOneQualifiedLookup(t *testing.T) {
	for _, test := range []struct {
		name       string
		sentAge    time.Duration
		previous   time.Duration
		provesID   bool
		wantLookup int32
		wantRefund bool
	}{
		{name: "first point has not arrived", sentAge: 2 * time.Minute},
		{name: "regular point has no evidence", sentAge: 4 * time.Minute, wantLookup: 1},
		{name: "regular point already taken", sentAge: 4 * time.Minute, previous: 30 * time.Second},
		{name: "terminal point has no evidence", sentAge: 31 * time.Minute, wantLookup: 1, wantRefund: true},
		{name: "prior terminal ticket is still fresh", sentAge: 31 * time.Minute, previous: 30 * time.Second},
		{name: "expired terminal ticket can recover original ID", sentAge: 34 * time.Minute, previous: 3 * time.Minute, provesID: true, wantLookup: 1},
		{name: "expired terminal ticket confirms the original refund once", sentAge: 34 * time.Minute, previous: 3 * time.Minute, wantLookup: 1, wantRefund: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var logs, states atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Method != http.MethodGet {
					http.NotFound(w, req)
					return
				}
				switch req.URL.Path {
				case "/api/log/token":
					logs.Add(1)
					if test.provesID {
						_, _ = io.WriteString(w, `{"success":true,"data":[{"type":2,"request_id":"tracking-fixture-request","model_name":"fixture-model-480p","other":{"task_id":"tracking-fixture-video","request_path":"/v1/videos"}}]}`)
					} else {
						_, _ = io.WriteString(w, `{"success":true,"data":[]}`)
					}
				case "/v1/videos/tracking-fixture-video":
					states.Add(1)
					_, _ = io.WriteString(w, `{"id":"tracking-fixture-video","status":"completed","delivery_status":"ready"}`)
				default:
					http.NotFound(w, req)
				}
			}))
			t.Cleanup(srv.Close)
			ctx := context.Background()
			task, repo, tracker := newTrackingVideoRelayFixture(t, srv.URL, 40*time.Minute)
			_, err := integrationDB.ExecContext(ctx, `
				UPDATE video_relay_tasks SET upstream_task_id='',status='unconfirmed',
					sent_at=clock_timestamp()-make_interval(secs => $2),
					last_lookup_at=CASE WHEN $3::double precision>0 THEN clock_timestamp()-make_interval(secs => $3) END
				WHERE task_id=$1`, task.TaskID, test.sentAge.Seconds(), test.previous.Seconds())
			require.NoError(t, err)
			claim, err := repo.ClaimDue(ctx)
			require.NoError(t, err)
			require.NotNil(t, claim)
			require.Equal(t, task.TaskID, claim.TaskID)
			tracker.ProcessClaim(ctx, claim)
			require.Equal(t, test.wantLookup, logs.Load())
			require.Zero(t, states.Load(), "recovering a number must leave its status check for the next claim")
			current, err := repo.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
			require.NoError(t, err)
			if test.wantRefund {
				require.Equal(t, service.VideoRelayRefunded, current.BillingState)
				require.Equal(t, service.VideoRelayUnconfirmed, current.FailureReason)
				require.NotNil(t, current.NextCheckAt)
				require.WithinDuration(t, current.SettledAt.Add(10*time.Minute), *current.NextCheckAt, time.Millisecond)
				assertVideoRelayFunds(t, integrationDB, task, 10, 0, 0)
				tracker.ProcessClaim(ctx, claim)
				require.Equal(t, test.wantLookup, logs.Load())
				assertVideoRelayFunds(t, integrationDB, task, 10, 0, 0)
				return
			}
			require.Equal(t, service.VideoRelayHeld, current.BillingState)
			assertVideoRelayFunds(t, integrationDB, task, 5, 5, 0)
			if !test.provesID {
				require.Empty(t, current.UpstreamTaskID)
				return
			}
			require.Equal(t, "tracking-fixture-video", current.UpstreamTaskID)
			makeVideoRelayTaskDue(t, integrationDB, task.TaskID)
			next, err := repo.ClaimDue(ctx)
			require.NoError(t, err)
			require.NotNil(t, next)
			require.Equal(t, task.TaskID, next.TaskID)
			tracker.ProcessClaim(ctx, next)
			require.EqualValues(t, 1, logs.Load())
			require.EqualValues(t, 1, states.Load())
			assertVideoRelayFunds(t, integrationDB, task, 5, 0, 1)
		})
	}
}

const trackingFixtureChargeLog = `{"success":true,"data":[{"type":2,"request_id":"tracking-fixture-request","model_name":"fixture-model-480p","other":{"task_id":"tracking-fixture-video","request_path":"/v1/videos"}}]}`

func TestVideoRelayRepoTerminalLookupHandoverRejectsOldEvidence(t *testing.T) {
	var received, confirmations atomic.Int32
	evidenceReady := make(chan struct{})
	resumeOld := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet || req.URL.Path != "/api/log/token" {
			http.NotFound(w, req)
			return
		}
		received.Add(1)
		_, _ = io.WriteString(w, trackingFixtureChargeLog)
	}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	task, repo, tracker := newTrackingVideoRelayFixture(t, srv.URL, 40*time.Minute, trackingVideoRelayFixtureOptions{
		wrapRepo: func(repo service.VideoRelayRepository) service.VideoRelayRepository {
			return videoRelayDelayedConfirmation{VideoRelayRepository: repo, beforeConfirm: func(ctx context.Context, _ *service.VideoRelayTask) {
				if confirmations.Add(1) == 1 {
					close(evidenceReady)
					select {
					case <-resumeOld:
					case <-ctx.Done():
					}
				}
			}}
		},
	})
	db2 := videoRelayIndependentDB(t)
	other := NewVideoRelayRepository(db2)
	_, err := db2.ExecContext(ctx, `
		UPDATE video_relay_tasks SET upstream_task_id='',status='unconfirmed',
		    sent_at=clock_timestamp()-interval '35 minutes',last_lookup_at=NULL
		WHERE task_id=$1`, task.TaskID)
	require.NoError(t, err)
	first, err := repo.ClaimDue(ctx)
	require.NoError(t, err)
	require.NotNil(t, first)
	require.Equal(t, task.TaskID, first.TaskID)
	oldDone := make(chan struct{})
	go func() {
		defer close(oldDone)
		tracker.ProcessClaim(ctx, first)
	}()
	select {
	case <-evidenceReady:
	case <-time.After(5 * time.Second):
		t.Fatal("old worker never received the terminal log evidence")
	}

	makeVideoRelayTaskDue(t, db2, task.TaskID)
	tooEarly, err := other.ClaimDue(ctx)
	require.NoError(t, err)
	require.NotNil(t, tooEarly)
	require.Equal(t, task.TaskID, tooEarly.TaskID)
	tracker.ProcessClaim(ctx, tooEarly)
	current, err := other.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
	require.NoError(t, err)
	require.NotNil(t, current.LastLookupAt)
	require.NotNil(t, current.NextCheckAt)
	require.WithinDuration(t, current.LastLookupAt.Add(2*time.Minute), *current.NextCheckAt, time.Millisecond)
	require.EqualValues(t, 1, received.Load(), "a fresh terminal qualification must prevent an extra log request")

	_, err = db2.ExecContext(ctx, `
		UPDATE video_relay_tasks SET last_lookup_at=clock_timestamp()-interval '121 seconds'
		WHERE task_id=$1`, task.TaskID)
	require.NoError(t, err)
	makeVideoRelayTaskDue(t, db2, task.TaskID)
	recovered, err := other.ClaimDue(ctx)
	require.NoError(t, err)
	require.NotNil(t, recovered)
	require.Equal(t, task.TaskID, recovered.TaskID)
	close(resumeOld)
	select {
	case <-oldDone:
	case <-time.After(5 * time.Second):
		t.Fatal("old worker did not return")
	}
	current, err = other.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
	require.NoError(t, err)
	require.Empty(t, current.UpstreamTaskID, "old evidence must not write after a new worker obtains the lease")
	require.Equal(t, recovered.TrackingLease, current.NextCheckAt, "old evidence must not replace the new worker's lease")
	assertVideoRelayFunds(t, db2, task, 5, 5, 0)

	tracker.ProcessClaim(ctx, recovered)
	current, err = other.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
	require.NoError(t, err)
	require.Equal(t, "tracking-fixture-video", current.UpstreamTaskID)
	require.EqualValues(t, 2, received.Load(), "one recovery lookup may supplement the interrupted lookup")
	assertVideoRelayFunds(t, db2, task, 5, 5, 0)
}

type videoRelayInterruptQualifiedLookup struct {
	service.VideoRelayRepository
	cancel context.CancelFunc
}

func (r *videoRelayInterruptQualifiedLookup) ClaimTrackingLookup(ctx context.Context, claim *service.VideoRelayTask) (*service.VideoRelayTask, bool, error) {
	ticket, qualified, err := r.VideoRelayRepository.ClaimTrackingLookup(ctx, claim)
	if err == nil && qualified && r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	return ticket, qualified, err
}

func TestVideoRelayRepoUnknownDeadlineRecoversAfterQualification(t *testing.T) {
	for _, beforeRequest := range []bool{true, false} {
		name := "failed confirmation schedules five-minute retry"
		if beforeRequest {
			name = "interruption after qualification leaves original lease without a GET"
		}
		t.Run(name, func(t *testing.T) {
			var logs atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Method != http.MethodGet || req.URL.Path != "/api/log/token" {
					http.NotFound(w, req)
					return
				}
				logs.Add(1)
				_, _ = io.WriteString(w, trackingFixtureChargeLog)
			}))
			t.Cleanup(srv.Close)
			ctx := context.Background()
			oldWorker, cancel := context.WithCancel(ctx)
			defer cancel()
			var confirmingAt time.Time
			task, repo, tracker := newTrackingVideoRelayFixture(t, srv.URL, 40*time.Minute, trackingVideoRelayFixtureOptions{
				wrapRepo: func(repo service.VideoRelayRepository) service.VideoRelayRepository {
					if beforeRequest {
						return &videoRelayInterruptQualifiedLookup{VideoRelayRepository: repo, cancel: cancel}
					}
					return videoRelayDelayedConfirmation{VideoRelayRepository: repo, beforeConfirm: func(ctx context.Context, _ *service.VideoRelayTask) {
						if confirmingAt.IsZero() {
							_, err := integrationDB.ExecContext(ctx, "SELECT pg_sleep(1.2)")
							require.NoError(t, err)
							require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&confirmingAt))
						}
					}}
				},
			})
			_, err := integrationDB.ExecContext(ctx, `
				UPDATE video_relay_tasks SET upstream_task_id='',status='unconfirmed',
				    sent_at=clock_timestamp()-interval '35 minutes',last_lookup_at=NULL
				WHERE task_id=$1`, task.TaskID)
			require.NoError(t, err)
			const clearConstraint = "ALTER TABLE video_relay_tasks DROP CONSTRAINT IF EXISTS fixture_block_video_confirmation"
			if !beforeRequest {
				_, err := integrationDB.ExecContext(ctx, fmt.Sprintf(`
					ALTER TABLE video_relay_tasks ADD CONSTRAINT fixture_block_video_confirmation
					CHECK (task_id <> %s OR upstream_task_id = '') NOT VALID`, pq.QuoteLiteral(task.TaskID)))
				require.NoError(t, err)
				t.Cleanup(func() {
					_, err := integrationDB.Exec(clearConstraint)
					require.NoError(t, err)
				})
			}
			first, err := repo.ClaimDue(ctx)
			require.NoError(t, err)
			require.NotNil(t, first)
			require.Equal(t, task.TaskID, first.TaskID)
			tracker.ProcessClaim(oldWorker, first)
			db2 := videoRelayIndependentDB(t)
			other := NewVideoRelayRepository(db2)
			current, err := other.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
			require.NoError(t, err)
			require.NotNil(t, current.LastLookupAt, "the real qualification transaction had already committed")
			require.Empty(t, current.UpstreamTaskID)
			require.Equal(t, service.VideoRelayHeld, current.BillingState)
			require.NotNil(t, current.NextCheckAt)
			want := int32(1)
			if beforeRequest {
				want = 0
				require.Equal(t, first.TrackingLease, current.NextCheckAt)
			} else {
				require.Greater(t, confirmingAt.Sub(*current.LastLookupAt), time.Second,
					"the real failed write must be separated from the original qualification")
				var afterFailure time.Time
				require.NoError(t, db2.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&afterFailure))
				require.False(t, current.NextCheckAt.Before(confirmingAt.Add(5*time.Minute)),
					"retry must be scheduled from the failure of the write")
				require.False(t, current.NextCheckAt.After(afterFailure.Add(5*time.Minute)))
			}
			require.Equal(t, want, logs.Load())
			assertVideoRelayFunds(t, db2, task, 5, 5, 0)
			early, err := other.ClaimDue(ctx)
			require.NoError(t, err)
			if early != nil {
				require.NotEqual(t, task.TaskID, early.TaskID)
			}
			require.Equal(t, want, logs.Load())
			if !beforeRequest {
				_, err := db2.ExecContext(ctx, clearConstraint)
				require.NoError(t, err)
			}

			_, err = db2.ExecContext(ctx, "UPDATE video_relay_tasks SET last_lookup_at=clock_timestamp()-interval '5 minutes' WHERE task_id=$1", task.TaskID)
			require.NoError(t, err)
			makeVideoRelayTaskDue(t, db2, task.TaskID)
			recovered, err := other.ClaimDue(ctx)
			require.NoError(t, err)
			require.NotNil(t, recovered)
			require.Equal(t, task.TaskID, recovered.TaskID)
			tracker.ProcessClaim(ctx, recovered)
			current, err = other.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
			require.NoError(t, err)
			require.Equal(t, "tracking-fixture-video", current.UpstreamTaskID)
			require.Equal(t, want+1, logs.Load())
			assertVideoRelayFunds(t, db2, task, 5, 5, 0)
		})
	}
}

type lateVideoRelayFixtureLog struct {
	service.HTTPUpstream
	calls atomic.Int32
}

func (f *lateVideoRelayFixtureLog) Do(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	f.calls.Add(1)
	<-request.Context().Done()
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(trackingFixtureChargeLog))}, nil
}

func TestVideoRelayRepoRefundLookupStopsAtTwoHours(t *testing.T) {
	for _, test := range []struct {
		name         string
		refundAge    time.Duration
		late         bool
		wantRealGET  int32
		wantLateGET  int32
		wantProvider string
	}{
		{name: "new old order still waits ten minutes from refund", refundAge: 9 * time.Minute},
		{name: "first point retrieves the number but keeps the refund", refundAge: 10*time.Minute + time.Second, wantRealGET: 1, wantProvider: "tracking-fixture-video"},
		{name: "last application lookup can recover the number", refundAge: 110*time.Minute + time.Second, wantRealGET: 1, wantProvider: "tracking-fixture-video"},
		{name: "two hour limit performs zero upstream calls", refundAge: 120 * time.Minute},
		{name: "late complete response cannot restore the number", refundAge: 120*time.Minute - 2*time.Second, late: true, wantLateGET: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var realGET atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Method != http.MethodGet || req.URL.Path != "/api/log/token" {
					http.NotFound(w, req)
					return
				}
				realGET.Add(1)
				_, _ = io.WriteString(w, trackingFixtureChargeLog)
			}))
			t.Cleanup(srv.Close)
			late := &lateVideoRelayFixtureLog{}
			var options trackingVideoRelayFixtureOptions
			if test.late {
				options.upstream = late
			}
			ctx := context.Background()
			task, repo, tracker := newTrackingVideoRelayFixture(t, srv.URL, 6*time.Hour, options)
			prepareVideoRelayRefundLookup(t, repo, task, test.refundAge)
			claim, err := repo.ClaimDue(ctx)
			require.NoError(t, err)
			require.NotNil(t, claim)
			require.Equal(t, task.TaskID, claim.TaskID)
			tracker.ProcessClaim(ctx, claim)
			current, err := repo.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
			require.NoError(t, err)
			require.Equal(t, test.wantRealGET, realGET.Load())
			require.Equal(t, test.wantLateGET, late.calls.Load())
			require.Equal(t, test.wantProvider, current.UpstreamTaskID)
			require.Equal(t, service.VideoRelayRefunded, current.BillingState)
			require.Equal(t, service.VideoRelayUnconfirmed, current.FailureReason)
			require.False(t, current.CanDownload())
			if test.refundAge >= 120*time.Minute || test.wantProvider != "" {
				require.Nil(t, current.NextCheckAt)
			}
			if test.late && current.NextCheckAt != nil {
				require.Equal(t, current.SettledAt.Add(2*time.Hour), *current.NextCheckAt)
			}
			assertVideoRelayFunds(t, integrationDB, task, 10, 0, 0)
		})
	}
}

type videoRelayDelayedConfirmation struct {
	service.VideoRelayRepository
	beforeConfirm func(context.Context, *service.VideoRelayTask)
}

func (r videoRelayDelayedConfirmation) ConfirmLookup(ctx context.Context, task *service.VideoRelayTask, path string, records []service.VideoRelayChargeRecord) (*service.VideoRelayTask, error) {
	r.beforeConfirm(ctx, task)
	return r.VideoRelayRepository.ConfirmLookup(ctx, task, path, records)
}

func TestVideoRelayRepoRefundLookupCanSaveEvidenceReceivedBeforeDeadline(t *testing.T) {
	var received atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet || req.URL.Path != "/api/log/token" {
			http.NotFound(w, req)
			return
		}
		received.Add(1)
		_, _ = io.WriteString(w, trackingFixtureChargeLog)
	}))
	t.Cleanup(srv.Close)
	confirmations := 0
	db2 := videoRelayIndependentDB(t)
	task, repo, tracker := newTrackingVideoRelayFixture(t, srv.URL, 6*time.Hour, trackingVideoRelayFixtureOptions{
		wrapRepo: func(repo service.VideoRelayRepository) service.VideoRelayRepository {
			return videoRelayDelayedConfirmation{VideoRelayRepository: repo, beforeConfirm: func(ctx context.Context, ticket *service.VideoRelayTask) {
				confirmations++
				cutoff := ticket.SettledAt.Add(service.VideoRelayRefundTrackingWindow)
				var now time.Time
				require.NoError(t, db2.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&now))
				require.True(t, now.Before(cutoff), "the full response must reach the tracker before the two-hour cutoff")
				_, err := db2.ExecContext(ctx, "SELECT pg_sleep(2)")
				require.NoError(t, err)
				require.NoError(t, db2.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&now))
				require.True(t, now.After(cutoff), "persist the evidence after cutoff while the original lease remains valid")
			}}
		},
	})
	prepareVideoRelayRefundLookup(t, repo, task, 120*time.Minute-1200*time.Millisecond)
	ctx := context.Background()
	claim, err := repo.ClaimDue(ctx)
	require.NoError(t, err)
	require.NotNil(t, claim)
	require.Equal(t, task.TaskID, claim.TaskID)
	tracker.ProcessClaim(ctx, claim)
	require.EqualValues(t, 1, received.Load())
	require.Equal(t, 1, confirmations)
	current, err := repo.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
	require.NoError(t, err)
	require.Equal(t, service.VideoRelayRefunded, current.BillingState)
	require.Equal(t, "tracking-fixture-video", current.UpstreamTaskID)
	require.False(t, current.CanDownload())
	require.Nil(t, current.NextCheckAt)
	assertVideoRelayFunds(t, db2, task, 10, 0, 0)
}

func TestVideoRelayRepoRefundedDeletedSourcesCanRecoverNumber(t *testing.T) {
	for _, reason := range []string{service.VideoRelayUserDeleted, service.VideoRelayAPIKeyDeleted} {
		t.Run(reason, func(t *testing.T) {
			var logs atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Method != http.MethodGet || req.URL.Path != "/api/log/token" {
					http.NotFound(w, req)
					return
				}
				logs.Add(1)
				_, _ = io.WriteString(w, trackingFixtureChargeLog)
			}))
			t.Cleanup(srv.Close)
			ctx := context.Background()
			task, repo, tracker := newTrackingVideoRelayFixture(t, srv.URL, 6*time.Hour)
			if reason == service.VideoRelayUserDeleted {
				_, err := integrationDB.ExecContext(ctx, "UPDATE users SET deleted_at=clock_timestamp() WHERE id=$1", task.UserID)
				require.NoError(t, err)
			} else {
				_, err := integrationDB.ExecContext(ctx, "UPDATE api_keys SET deleted_at=clock_timestamp() WHERE id=$1", task.APIKeyID)
				require.NoError(t, err)
			}
			prepareVideoRelayRefundLookup(t, repo, task, 11*time.Minute)
			refunded, err := repo.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
			require.NoError(t, err)
			require.Equal(t, reason, refunded.FailureReason)
			assertVideoRelayFunds(t, integrationDB, task, 10, 0, 0)

			next := newVideoRelayTask(t, 1, 0)
			next.SiteFingerprint, next.CredentialFingerprint, next.RouteFingerprint = task.SiteFingerprint, task.CredentialFingerprint, task.RouteFingerprint
			require.NoError(t, repo.Reserve(ctx, &next))
			_, err = repo.AcquireSend(ctx, &next)
			require.ErrorIs(t, err, service.ErrVideoRelaySendBlocked, "the original dedicated line remains paused without an upstream task number")

			claim, err := repo.ClaimDue(ctx)
			require.NoError(t, err)
			require.NotNil(t, claim)
			require.Equal(t, task.TaskID, claim.TaskID)
			tracker.ProcessClaim(ctx, claim)
			current, err := repo.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
			require.NoError(t, err)
			require.EqualValues(t, 1, logs.Load())
			require.Equal(t, "tracking-fixture-video", current.UpstreamTaskID)
			require.Equal(t, reason, current.FailureReason)
			require.Equal(t, refunded.SettledAt, current.SettledAt)
			require.Equal(t, service.VideoRelayRefunded, current.BillingState)
			require.False(t, current.CanDownload())
			assertVideoRelayFunds(t, integrationDB, task, 10, 0, 0)
			sent, err := repo.AcquireSend(ctx, &next)
			require.NoError(t, err, "persisting the original number clears only the line's send pause")
			_, _, err = repo.Settle(ctx, sent, service.VideoRelaySettlement{FailureReason: service.VideoRelayNotSent})
			require.NoError(t, err)
		})
	}
}

func prepareVideoRelayRefundLookup(t *testing.T, repo service.VideoRelayRepository, task service.VideoRelayTask, refundAge time.Duration) {
	t.Helper()
	ctx := context.Background()
	_, err := integrationDB.ExecContext(ctx, `
		UPDATE video_relay_tasks SET upstream_task_id='',status='unconfirmed',
			sent_at=clock_timestamp()-interval '35 minutes',last_lookup_at=clock_timestamp()
		WHERE task_id=$1`, task.TaskID)
	require.NoError(t, err)
	ticket, err := repo.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
	require.NoError(t, err)
	refunded, _, err := repo.Settle(ctx, ticket, service.VideoRelaySettlement{FailureReason: service.VideoRelayUnconfirmed})
	require.NoError(t, err)
	require.Equal(t, service.VideoRelayRefunded, refunded.BillingState)
	_, err = integrationDB.ExecContext(ctx, `
		UPDATE video_relay_tasks SET settled_at=clock_timestamp()-make_interval(secs => $2),last_lookup_at=NULL
		WHERE task_id=$1`, task.TaskID, refundAge.Seconds())
	require.NoError(t, err)
}

type videoRelayTrackingCounts struct {
	service.VideoRelayRepository
	finishes    atomic.Int32
	sourceReads atomic.Int32
}

func (r *videoRelayTrackingCounts) FinishClaim(ctx context.Context, task *service.VideoRelayTask, finish service.VideoRelayTrackingFinish) (*service.VideoRelayTask, error) {
	r.finishes.Add(1)
	return r.VideoRelayRepository.FinishClaim(ctx, task, finish)
}

// Instrument the existing extra operation before removal. It is not used by
// the repository when the deleted-source result comes with the lease.
func (r *videoRelayTrackingCounts) TrackingSourceRemoval(ctx context.Context, task *service.VideoRelayTask) (string, error) {
	r.sourceReads.Add(1)
	return r.VideoRelayRepository.(interface {
		TrackingSourceRemoval(context.Context, *service.VideoRelayTask) (string, error)
	}).TrackingSourceRemoval(ctx, task)
}

func TestVideoRelayRepoRetiredWorkerDoesNotOpenFinishTransaction(t *testing.T) {
	for _, stage := range []string{"qualification", "confirmation"} {
		t.Run(stage, func(t *testing.T) {
			ctx := context.Background()
			var reads atomic.Int32
			started, release := make(chan struct{}), make(chan struct{})
			stopped := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/log/token" || r.Method != http.MethodGet {
					http.NotFound(w, r)
					return
				}
				if reads.Add(1) == 1 {
					close(started)
				}
				<-release
				_, _ = io.WriteString(w, trackingFixtureChargeLog)
			}))
			t.Cleanup(srv.Close)
			t.Cleanup(func() {
				if !stopped {
					close(release)
				}
			})
			counter := &videoRelayTrackingCounts{}
			task, repo, tracker := newTrackingVideoRelayFixture(t, srv.URL, 20*time.Minute, trackingVideoRelayFixtureOptions{
				wrapRepo: func(actual service.VideoRelayRepository) service.VideoRelayRepository {
					counter.VideoRelayRepository = actual
					return counter
				},
			})
			_, err := integrationDB.ExecContext(ctx, `
			UPDATE video_relay_tasks SET upstream_task_id='',status='unconfirmed',
				sent_at=clock_timestamp()-interval '9 minutes' WHERE task_id=$1`, task.TaskID)
			require.NoError(t, err)
			old, err := repo.ClaimDue(ctx)
			require.NoError(t, err)
			require.NotNil(t, old)
			require.Equal(t, task.TaskID, old.TaskID)
			db2 := videoRelayIndependentDB(t)
			replacement := NewVideoRelayRepository(db2)
			done := make(chan struct{})
			if stage == "confirmation" {
				go func() { tracker.ProcessClaim(ctx, old); close(done) }()
				select {
				case <-started:
				case <-time.After(3 * time.Second):
					t.Fatal("recorded claim did not reach original billing log")
				}
			}
			makeVideoRelayTaskDue(t, db2, task.TaskID)
			current, err := replacement.ClaimDue(ctx)
			require.NoError(t, err)
			require.NotNil(t, current)
			require.Equal(t, task.TaskID, current.TaskID)
			require.NotEqual(t, current.NextCheckAt, old.NextCheckAt)
			close(release)
			stopped = true
			if stage == "qualification" {
				tracker.ProcessClaim(ctx, old)
				require.Zero(t, reads.Load())
			} else {
				select {
				case <-done:
				case <-time.After(3 * time.Second):
					t.Fatal("superseded log processing did not return")
				}
				require.EqualValues(t, 1, reads.Load())
			}
			require.Zero(t, counter.finishes.Load(), "a known lost lease must not start another transaction")
			latest, err := replacement.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
			require.NoError(t, err)
			require.Equal(t, current.NextCheckAt, latest.NextCheckAt)
			require.Empty(t, latest.UpstreamTaskID)
			require.Equal(t, service.VideoRelayHeld, latest.BillingState)
			assertVideoRelayFunds(t, db2, task, 5, 5, 0)
		})
	}
}

func TestVideoRelayRepoClaimUsesSourceDecisionWithoutAnotherQuery(t *testing.T) {
	ctx := context.Background()
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	counter := &videoRelayTrackingCounts{}
	task, repo, tracker := newTrackingVideoRelayFixture(t, srv.URL, 10*time.Minute, trackingVideoRelayFixtureOptions{
		wrapRepo: func(actual service.VideoRelayRepository) service.VideoRelayRepository {
			counter.VideoRelayRepository = actual
			return counter
		},
	})
	_, err := integrationDB.ExecContext(ctx, "UPDATE accounts SET deleted_at=clock_timestamp() WHERE id=$1", task.AccountID)
	require.NoError(t, err)
	claim, err := repo.ClaimDue(ctx)
	require.NoError(t, err)
	require.NotNil(t, claim)
	require.Equal(t, task.TaskID, claim.TaskID)
	tracker.ProcessClaim(ctx, claim)
	require.Zero(t, counter.sourceReads.Load(), "the claim must carry its existing deletion decision")
	require.Zero(t, posts.Load())
	finished, err := repo.Get(ctx, task.TaskID, task.UserID, task.APIKeyID)
	require.NoError(t, err)
	require.Equal(t, service.VideoRelayAccountDeleted, finished.FailureReason)
	require.Equal(t, service.VideoRelayRefunded, finished.BillingState)
	assertVideoRelayFunds(t, integrationDB, task, 10, 0, 0)
}
