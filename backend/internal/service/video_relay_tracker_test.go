//go:build unit

package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

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
