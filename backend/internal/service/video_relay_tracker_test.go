//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type videoRelayTrackingIdentityStore struct {
	VideoRelayRepository
	lookups, finished int
}

func (r *videoRelayTrackingIdentityStore) ClaimTrackingLookup(_ context.Context, task *VideoRelayTask) (*VideoRelayTask, bool, error) {
	r.lookups++
	ticket := *task
	now := time.Now()
	ticket.LastLookupAt = &now
	return &ticket, true, nil
}

func (r *videoRelayTrackingIdentityStore) FinishClaim(_ context.Context, task *VideoRelayTask, _ VideoRelayTrackingFinish) (*VideoRelayTask, error) {
	r.finished++
	return task, nil
}

func TestVideoRelayTrackerUsesTaskIdentityForBothActualRequests(t *testing.T) {
	received := make(chan *http.Request, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		received <- req.Clone(context.Background())
		w.Header().Set("Content-Type", "application/json")
		if req.URL.Path == "/api/log/token" {
			_, _ = io.WriteString(w, `{"success":true,"data":[]}`)
		} else {
			_, _ = io.WriteString(w, `{"id":"provider-task","status":"in_progress"}`)
		}
	}))
	t.Cleanup(upstream.Close)
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Security.URLAllowlist.AllowPrivateHosts = true
	account := &Account{ID: 21, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"base_url": upstream.URL, "api_key": "fixture",
		credKeyHeaderOverrideEnabled: true,
		credKeyHeaderOverrides: map[string]any{
			"x-user": "{{user_id}}", "x-key": "{{api_key_id}}", "x-group": "{{group_id}}", "x-trace": "{{request_id}}",
		},
	}}
	accounts := &videoRelayIdentityAccounts{account: account}
	gateway := &OpenAIGatewayService{cfg: cfg, accountRepo: accounts}
	parent, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	parent = context.WithValue(parent, ctxkey.RequestID, "unrelated-parent")
	wrongGroup := int64(999)
	parent = WithHeaderOverrideRequestScope(parent, 999, 999, &wrongGroup)
	deadline, _ := parent.Deadline()
	gateway.httpUpstream = videoRelayTestTransport{client: upstream.Client(), request: func(req *http.Request) {
		actual, ok := req.Context().Deadline()
		require.True(t, ok)
		require.Equal(t, deadline, actual, "identity wrappers must retain the earlier caller deadline")
		require.Nil(t, VideoRelayTrackingCanceled(req.Context()))
	}}
	route, err := gateway.videoRelayRoute(account)
	require.NoError(t, err)
	store := &videoRelayTrackingIdentityStore{}
	tracker := NewVideoRelayTracker(NewVideoRelayService(store, gateway, nil))
	now := time.Now()
	sent := now.Add(-time.Second)
	task := &VideoRelayTask{
		TaskID: "video_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", UserID: 7, APIKeyID: 31, GroupID: 9, AccountID: account.ID,
		BillingState: VideoRelayHeld, SentAt: &sent, CreatedAt: now, TrackingStartedAt: now,
		SiteFingerprint: route.site, CredentialFingerprint: route.credential, RouteFingerprint: route.fingerprint,
	}
	seenIDs := make(map[string]bool)
	for _, known := range []bool{true, false} {
		for range 2 {
			task.UpstreamTaskID = ""
			expectedPath := "/api/log/token"
			if known {
				task.UpstreamTaskID = "provider-task"
				expectedPath = "/v1/videos/provider-task"
			}
			tracker.ProcessClaim(parent, task)
			var req *http.Request
			select {
			case req = <-received:
			case <-time.After(time.Second):
				t.Fatal("tracking path did not send its actual upstream request")
			}
			require.Equal(t, expectedPath, req.URL.Path)
			require.Equal(t, "7", req.Header.Get("X-User"))
			require.Equal(t, "31", req.Header.Get("X-Key"))
			require.Equal(t, "9", req.Header.Get("X-Group"))
			requestID := req.Header.Get("X-Trace")
			_, err := uuid.Parse(requestID)
			require.NoError(t, err)
			require.False(t, seenIDs[requestID], "each actual request needs a fresh ID")
			seenIDs[requestID] = true
		}
	}
	require.Equal(t, 4, accounts.reads, "no extra user, key or group lookup")
	require.Equal(t, 2, store.lookups)
	require.Equal(t, 4, store.finished)
	require.Equal(t, "unrelated-parent", parent.Value(ctxkey.RequestID))
	require.Equal(t, int64(999), headerOverrideScopeFromContext(parent).userID)
	cancel()
	tracker.ProcessClaim(parent, task)
	require.Equal(t, 4, accounts.reads, "canceled work must not reach the account or upstream")
	require.Empty(t, received)
}

func TestVideoRelayTrackingIdentityContextPreservesCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.WithValue(context.Background(), videoRelayTrackingContextKey{}, true))
	ctx := videoRelayTrackingRequestContext(parent, &VideoRelayTask{UserID: 7, APIKeyID: 31, GroupID: 9})
	cancel()
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.ErrorIs(t, VideoRelayTrackingCanceled(ctx), context.Canceled)
	require.Equal(t, int64(9), headerOverrideScopeFromContext(ctx).groupID)
}

type videoRelayTrackingRoundFixture struct {
	VideoRelayRepository
	count, available, finished int
}

func (r *videoRelayTrackingRoundFixture) ClaimDue(context.Context) (*VideoRelayTask, error) {
	r.count++
	if r.count > r.available {
		return nil, nil
	}
	return &VideoRelayTask{BillingState: VideoRelayCharged}, nil
}

func (r *videoRelayTrackingRoundFixture) FinishClaim(_ context.Context, task *VideoRelayTask, _ VideoRelayTrackingFinish) (*VideoRelayTask, error) {
	r.finished++
	return task, nil
}

func TestVideoRelayTrackingRoundDelays(t *testing.T) {
	for _, test := range []struct {
		available, wantedClaims, wantedFinished int
		wait                                    time.Duration
	}{
		{0, 1, 0, 30 * time.Second},
		{3, 4, 3, 10 * time.Second},
		{40, 20, 20, 10 * time.Second},
	} {
		repo := &videoRelayTrackingRoundFixture{available: test.available}
		tracker := NewVideoRelayTracker(NewVideoRelayService(repo, nil, nil))
		require.Equal(t, test.wait, tracker.runRound(context.Background()))
		require.Equal(t, test.wantedClaims, repo.count)
		require.Equal(t, test.wantedFinished, repo.finished)
	}
}

type videoRelayTrackingStopFixture struct {
	VideoRelayRepository
	calls                       atomic.Int32
	started, aborted, unblocked chan struct{}
}

func (r *videoRelayTrackingStopFixture) ClaimDue(ctx context.Context) (*VideoRelayTask, error) {
	if r.calls.Add(1) != 1 {
		return nil, errors.New("unexpected second claim")
	}
	close(r.started)
	<-ctx.Done()
	close(r.aborted)
	<-r.unblocked
	return nil, ctx.Err()
}

func TestVideoRelayTrackingStopWaitsForActualExit(t *testing.T) {
	repo := &videoRelayTrackingStopFixture{started: make(chan struct{}), aborted: make(chan struct{}), unblocked: make(chan struct{})}
	tracker := NewVideoRelayTracker(NewVideoRelayService(repo, nil, nil))
	tracker.Start()
	tracker.Start()
	released := false
	defer func() {
		if !released {
			close(repo.unblocked)
		}
		tracker.Stop()
	}()
	select {
	case <-repo.started:
	case <-time.After(time.Second):
		t.Fatal("background claim did not start")
	}
	stopped := make(chan struct{})
	go func() {
		tracker.Stop()
		close(stopped)
	}()
	select {
	case <-repo.aborted:
	case <-time.After(time.Second):
		t.Fatal("Stop did not cancel the outstanding query")
	}
	select {
	case <-stopped:
		t.Fatal("Stop returned while the database operation was still running")
	case <-time.After(10 * time.Millisecond):
	}
	require.EqualValues(t, 1, repo.calls.Load())
	close(repo.unblocked)
	released = true
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop did not return after the outstanding query exited")
	}
	tracker.Stop()
}

func TestVideoRelayTrackingFixedLookupWindows(t *testing.T) {
	sent := time.Date(2026, 11, 24, 1, 0, 0, 0, time.UTC)
	task := &VideoRelayTask{BillingState: VideoRelayHeld, SentAt: &sent}
	for _, minute := range []int{3, 8, 13, 18, 23, 28, 30} {
		point := sent.Add(time.Duration(minute) * time.Minute)
		early, next := VideoRelayTrackingLookupPoints(task, point.Add(-time.Microsecond))
		require.True(t, early.Before(point))
		require.Equal(t, point, next)
		latest, _ := VideoRelayTrackingLookupPoints(task, point)
		require.Equal(t, point, latest)
	}
	settled := sent.Add(30 * time.Minute)
	task.BillingState, task.FailureReason, task.SettledAt = VideoRelayRefunded, VideoRelayUnconfirmed, &settled
	for minute := 10; minute < 120; minute += 10 {
		point := settled.Add(time.Duration(minute) * time.Minute)
		early, next := VideoRelayTrackingLookupPoints(task, point.Add(-time.Microsecond))
		require.True(t, early.Before(point))
		require.Equal(t, point, next)
		latest, _ := VideoRelayTrackingLookupPoints(task, point)
		require.Equal(t, point, latest)
	}
	cutoff := settled.Add(2 * time.Hour)
	latest, next := VideoRelayTrackingLookupPoints(task, cutoff)
	require.True(t, latest.IsZero())
	require.True(t, next.IsZero())
	require.Nil(t, VideoRelayNextCheck(task, cutoff, cutoff, false))
	task.FailureReason = VideoRelayUpstreamRejected
	require.False(t, task.CanTrackRefund())
}
