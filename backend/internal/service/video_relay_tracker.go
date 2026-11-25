package service

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
)

const (
	videoRelayBusyDelay   = 10 * time.Second
	videoRelayIdleDelay   = 30 * time.Second
	videoRelayRoundLimit  = 20
	videoRelayUpstreamTTL = 15 * time.Second
	videoRelayBusinessTTL = 25 * time.Second
	videoRelayCleanupTTL  = 30 * time.Second
)

type videoRelayTrackingContextKey struct{}

// VideoRelayTrackingCanceled lets a shared call stop before its next operation
// if a background video item has already reached its deadline.
func VideoRelayTrackingCanceled(ctx context.Context) error {
	if ctx == nil || ctx.Value(videoRelayTrackingContextKey{}) == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}

type VideoRelayTracker struct {
	relay   *VideoRelayService
	mu      sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
	stopped bool
}

func NewVideoRelayTracker(relay *VideoRelayService) *VideoRelayTracker {
	return &VideoRelayTracker{relay: relay}
}

func ProvideVideoRelayTracker(relay *VideoRelayService) *VideoRelayTracker {
	tracker := NewVideoRelayTracker(relay)
	tracker.Start()
	return tracker
}

func (t *VideoRelayTracker) Start() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.done != nil || t.stopped {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	t.done = make(chan struct{})
	go func() {
		defer close(t.done)
		for ctx.Err() == nil {
			timer := time.NewTimer(t.runRound(ctx))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}

func (t *VideoRelayTracker) Stop() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.stopped = true
	done := t.done
	if t.cancel != nil {
		t.cancel()
	}
	t.mu.Unlock()
	if done != nil {
		<-done
	}
}

func (t *VideoRelayTracker) runRound(ctx context.Context) time.Duration {
	delay := videoRelayIdleDelay
	for range videoRelayRoundLimit {
		if ctx.Err() != nil {
			break
		}
		claim, err := t.relay.repo.ClaimDue(ctx)
		if err != nil {
			if ctx.Err() == nil {
				logger.LegacyPrintf("service.video_relay", "background video claim failed")
			}
			break
		}
		if claim == nil {
			break
		}
		delay = videoRelayBusyDelay
		t.ProcessClaim(ctx, claim)
	}
	return delay
}

func (t *VideoRelayTracker) ProcessClaim(parent context.Context, claim *VideoRelayTask) {
	start := time.Now()
	parent = context.WithValue(parent, videoRelayTrackingContextKey{}, true)
	work, cancelWork := context.WithDeadline(parent, start.Add(videoRelayBusinessTTL))
	defer cancelWork()
	cleanup, cancelCleanup := context.WithDeadline(context.WithoutCancel(parent), start.Add(videoRelayCleanupTTL))
	defer cancelCleanup()
	upstream, cancelUpstream := context.WithDeadline(work, start.Add(videoRelayUpstreamTTL))
	defer cancelUpstream()
	if VideoRelayTrackingCanceled(work) != nil {
		return
	}

	switch {
	case claim.BillingState == VideoRelayHeld:
		if reason := claim.TrackingSourceRemoval; reason != "" {
			t.settle(work, cleanup, claim, claim, VideoRelaySettlement{FailureReason: reason})
			return
		}
	case !claim.CanTrackRefund():
		t.finish(work, claim, VideoRelayTrackingFinish{})
		return
	}
	switch {
	case claim.SentAt == nil:
		if !claim.TrackingStartedAt.Before(claim.CreatedAt.Add(VideoRelayUnsentDeadline)) {
			t.settle(work, cleanup, claim, claim, VideoRelaySettlement{
				FailureReason: VideoRelayNotSent, ErrorMessage: VideoRelayFailureMessage(VideoRelayNotSent),
			})
			return
		}
		t.finish(work, claim, VideoRelayTrackingFinish{})
	case ValidVideoRelayUpstreamTaskID(claim.UpstreamTaskID):
		t.trackKnown(work, upstream, cleanup, claim)
	default:
		t.trackUnknown(work, upstream, cleanup, claim)
	}
}

type videoRelayRouteReader interface {
	GetVideoRelayAccount(context.Context, int64) (*Account, error)
}

func (t *VideoRelayTracker) originalTrackingRoute(ctx, cleanup context.Context, task *VideoRelayTask) (*videoRelayRoute, func(), error) {
	if err := VideoRelayTrackingCanceled(ctx); err != nil {
		return nil, nil, err
	}
	gateway := t.relay.gateway
	var account *Account
	var err error
	if routeReader, ok := gateway.accountRepo.(videoRelayRouteReader); ok {
		account, err = routeReader.GetVideoRelayAccount(ctx, task.AccountID)
	} else {
		account, err = gateway.accountRepo.GetByID(ctx, task.AccountID)
	}
	if err != nil {
		return nil, nil, err
	}
	if VideoRelayTrackingCanceled(ctx) != nil {
		return nil, nil, nil
	}
	route, err := gateway.videoRelayRoute(account)
	if err != nil || !route.matches(task) || VideoRelayTrackingCanceled(ctx) != nil {
		return nil, nil, nil
	}
	if gateway.concurrencyService == nil {
		return route, nil, nil
	}
	slot, err := gateway.concurrencyService.acquireAccountSlot(ctx, cleanup, account.ID, account.Concurrency)
	if err != nil {
		return nil, nil, err
	}
	if !slot.Acquired {
		return nil, nil, nil
	}
	return route, slot.ReleaseFunc, nil
}

func videoRelayTrackingRequestContext(ctx context.Context, task *VideoRelayTask) context.Context {
	ctx = WithHeaderOverrideRequestScope(ctx, task.UserID, task.APIKeyID, &task.GroupID)
	return context.WithValue(ctx, ctxkey.RequestID, uuid.NewString())
}

func (t *VideoRelayTracker) trackKnown(work, upstream, cleanup context.Context, claim *VideoRelayTask) {
	expired := !claim.TrackingStartedAt.Before(claim.CreatedAt.Add(VideoRelayTrackingWindow))
	route, release, err := t.originalTrackingRoute(upstream, cleanup, claim)
	if release != nil {
		defer release()
	}
	if errors.Is(err, ErrAccountNotFound) {
		t.settle(work, cleanup, claim, claim, VideoRelaySettlement{FailureReason: VideoRelayAccountDeleted})
		return
	}
	if err != nil {
		t.retry(work, claim, "load original account or acquire its slot", err)
		return
	}
	var reply videoRelayReply
	if route != nil && VideoRelayTrackingCanceled(upstream) == nil {
		reply, err = t.readStatus(videoRelayTrackingRequestContext(upstream, claim), route, claim.UpstreamTaskID)
	}
	if err == nil && VideoRelayTrackingCanceled(upstream) == nil {
		switch reply.Status {
		case VideoRelayStatusCompleted:
			t.settle(work, cleanup, claim, claim, VideoRelaySettlement{Completed: true})
			return
		case VideoRelayStatusFailed:
			t.settle(work, cleanup, claim, claim, VideoRelaySettlement{
				FailureReason: VideoRelayUpstreamFailed, ErrorMessage: VideoRelayFailureMessage(VideoRelayUpstreamFailed),
			})
			return
		}
	}
	if expired {
		t.settle(work, cleanup, claim, claim, VideoRelaySettlement{
			FailureReason: VideoRelayTrackingExpired, ErrorMessage: VideoRelayFailureMessage(VideoRelayTrackingExpired),
		})
		return
	}
	if err != nil || VideoRelayTrackingCanceled(upstream) != nil {
		reply = videoRelayReply{}
	}
	t.finish(work, claim, VideoRelayTrackingFinish{Status: reply.Status, Progress: reply.Progress})
}

func (t *VideoRelayTracker) readStatus(ctx context.Context, route *videoRelayRoute, id string) (videoRelayReply, error) {
	request, err := route.taskRequest(ctx, id, false)
	if err != nil {
		return videoRelayReply{}, err
	}
	response, err := t.relay.gateway.httpUpstream.Do(request, route.proxyURL, route.account.ID, route.account.Concurrency)
	if err != nil {
		return videoRelayReply{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return videoRelayReply{}, errors.New("video status request did not return success")
	}
	body, err := readVideoRelayBody(response.Body, videoRelayResponseLimit)
	if err != nil {
		return videoRelayReply{}, err
	}
	return parseVideoRelayReply(body, id)
}

func (t *VideoRelayTracker) trackUnknown(work, upstream, cleanup context.Context, claim *VideoRelayTask) {
	if claim.CanTrackRefund() && !claim.TrackingStartedAt.Before(claim.SettledAt.Add(VideoRelayRefundTrackingWindow)) {
		t.finish(work, claim, VideoRelayTrackingFinish{})
		return
	}
	if VideoRelayTrackingCanceled(work) != nil {
		return
	}
	qualificationStart := time.Now()
	ticket, qualified, err := t.relay.repo.ClaimTrackingLookup(work, claim)
	if err != nil {
		t.retry(work, claim, "claim original billing log lookup", err)
		return
	}
	if !qualified {
		t.finish(work, claim, VideoRelayTrackingFinish{})
		return
	}
	if ticket.CanTrackRefund() {
		remaining := ticket.SettledAt.Add(VideoRelayRefundTrackingWindow).Sub(*ticket.LastLookupAt)
		var cancel context.CancelFunc
		upstream, cancel = context.WithDeadline(upstream, qualificationStart.Add(remaining))
		defer cancel()
	}
	expired := ticket.BillingState == VideoRelayHeld && !ticket.LastLookupAt.Before(ticket.SentAt.Add(VideoRelayConfirmationWindow))
	route, release, err := t.originalTrackingRoute(upstream, cleanup, ticket)
	if release != nil {
		defer release()
	}
	switch {
	case errors.Is(err, ErrAccountNotFound) && ticket.BillingState == VideoRelayRefunded:
		t.finish(work, claim, VideoRelayTrackingFinish{AccountRemoved: true})
		return
	case errors.Is(err, ErrAccountNotFound):
		t.settle(work, cleanup, claim, ticket, VideoRelaySettlement{FailureReason: VideoRelayAccountDeleted})
		return
	case err != nil:
		t.retry(work, claim, "load original account or acquire its slot", err)
		return
	}
	if route != nil && VideoRelayTrackingCanceled(upstream) == nil {
		logs, readErr := route.queryCharges(videoRelayTrackingRequestContext(upstream, ticket), t.relay.gateway.httpUpstream)
		if readErr == nil && len(logs) != 0 && VideoRelayTrackingCanceled(upstream) == nil && VideoRelayTrackingCanceled(work) == nil {
			updated, err := t.relay.repo.ConfirmLookup(work, ticket, route.collection.EscapedPath(), logs)
			if err != nil {
				t.retry(work, claim, "confirm original video submission", err)
				return
			}
			if ValidVideoRelayUpstreamTaskID(updated.UpstreamTaskID) || updated.BillingState != ticket.BillingState {
				t.finish(work, claim, VideoRelayTrackingFinish{})
				return
			}
		}
	}
	if expired {
		t.settle(work, cleanup, claim, ticket, VideoRelaySettlement{
			FailureReason: VideoRelayUnconfirmed, ErrorMessage: VideoRelayFailureMessage(VideoRelayUnconfirmed),
		})
		return
	}
	t.finish(work, claim, VideoRelayTrackingFinish{})
}

func (t *VideoRelayTracker) retry(ctx context.Context, claim *VideoRelayTask, step string, err error) {
	if errors.Is(err, ErrVideoRelayLeaseLost) {
		return
	}
	logger.LegacyPrintf("service.video_relay", "background video %s: %s failed", claim.TaskID, step)
	t.finish(ctx, claim, VideoRelayTrackingFinish{Retry: true})
}

func (t *VideoRelayTracker) finish(ctx context.Context, claim *VideoRelayTask, finish VideoRelayTrackingFinish) {
	if VideoRelayTrackingCanceled(ctx) != nil {
		logger.LegacyPrintf("service.video_relay", "background video %s: scheduling deadline reached", claim.TaskID)
		return
	}
	if _, err := t.relay.repo.FinishClaim(ctx, claim, finish); err != nil && !errors.Is(err, ErrVideoRelayLeaseLost) {
		logger.LegacyPrintf("service.video_relay", "background video %s: scheduling failed", claim.TaskID)
	}
}

func (t *VideoRelayTracker) settle(ctx, cleanup context.Context, claim, evidence *VideoRelayTask, change VideoRelaySettlement) {
	if VideoRelayTrackingCanceled(ctx) != nil {
		logger.LegacyPrintf("service.video_relay", "background video %s: settlement deadline reached", claim.TaskID)
		return
	}
	_, result, err := t.relay.settleVideoRelay(ctx, cleanup, evidence, change)
	if err != nil {
		t.retry(ctx, claim, "settle original video hold", err)
		return
	}
	if result == nil || !result.Applied {
		t.finish(ctx, claim, VideoRelayTrackingFinish{})
	}
}
