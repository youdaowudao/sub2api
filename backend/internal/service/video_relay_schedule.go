package service

import "time"

const (
	VideoRelayUnsentDeadline       = 10 * time.Minute
	VideoRelaySendGrace            = 3 * time.Minute
	VideoRelayTrackingWindow       = 5 * time.Hour
	VideoRelayRefundTrackingWindow = 2 * time.Hour
	VideoRelayTrackingLease        = 2 * time.Minute
	VideoRelayTrackingRetry        = 5 * time.Minute
)

var videoRelayHeldLookupMinutes = [...]int{3, 8, 13, 18, 23, 28, 30}

func (task *VideoRelayTask) CanTrackRefund() bool {
	if task.BillingState != VideoRelayRefunded || task.SentAt == nil || task.SettledAt == nil ||
		ValidVideoRelayUpstreamTaskID(task.UpstreamTaskID) {
		return false
	}
	return task.FailureReason == VideoRelayUnconfirmed || task.FailureReason == VideoRelayAPIKeyDeleted ||
		task.FailureReason == VideoRelayUserDeleted
}

// VideoRelayTrackingLookupPoints returns the most recent eligible point and
// the next one. A refunded order's final point only clears its schedule.
func VideoRelayTrackingLookupPoints(task *VideoRelayTask, now time.Time) (latest, next time.Time) {
	if task.SentAt == nil || ValidVideoRelayUpstreamTaskID(task.UpstreamTaskID) {
		return
	}
	if task.CanTrackRefund() {
		cutoff := task.SettledAt.Add(VideoRelayRefundTrackingWindow)
		if !now.Before(cutoff) {
			return
		}
		earliest := task.SentAt.Add(VideoRelaySendGrace)
		for minute := 10; minute < 120; minute += 10 {
			point := task.SettledAt.Add(time.Duration(minute) * time.Minute)
			if point.Before(earliest) {
				continue
			}
			if now.Before(point) {
				return latest, point
			}
			latest = point
		}
		return latest, cutoff
	}
	if task.BillingState == VideoRelayHeld {
		for _, minute := range videoRelayHeldLookupMinutes {
			point := task.SentAt.Add(time.Duration(minute) * time.Minute)
			if now.Before(point) {
				return latest, point
			}
			latest = point
		}
	}
	return
}

func VideoRelayNextCheck(task *VideoRelayTask, started, now time.Time, retry bool) *time.Time {
	if task.BillingState != VideoRelayHeld && !task.CanTrackRefund() {
		return nil
	}
	if retry && task.BillingState == VideoRelayHeld && task.SentAt != nil &&
		(ValidVideoRelayUpstreamTaskID(task.UpstreamTaskID) && !started.Before(task.CreatedAt.Add(VideoRelayTrackingWindow)) ||
			!ValidVideoRelayUpstreamTaskID(task.UpstreamTaskID) && !now.Before(task.SentAt.Add(VideoRelayConfirmationWindow))) {
		next := now.Add(VideoRelayTrackingRetry)
		return &next
	}
	var next time.Time
	switch {
	case task.BillingState == VideoRelayHeld && task.SentAt == nil:
		deadline := task.CreatedAt.Add(VideoRelayUnsentDeadline)
		next = now.Add(time.Minute)
		if started.Before(deadline) {
			next = earlierVideoRelayTime(next, deadline)
		}
	case task.BillingState == VideoRelayHeld && ValidVideoRelayUpstreamTaskID(task.UpstreamTaskID):
		deadline := task.CreatedAt.Add(VideoRelayTrackingWindow)
		delay := VideoRelayTrackingRetry
		if now.Before(task.CreatedAt.Add(30 * time.Minute)) {
			delay = 30 * time.Second
		}
		next = now.Add(delay)
		if started.Before(deadline) {
			next = earlierVideoRelayTime(next, deadline)
		}
	default:
		_, next = VideoRelayTrackingLookupPoints(task, now)
		if next.IsZero() && task.BillingState == VideoRelayHeld {
			if task.LastLookupAt != nil && !task.LastLookupAt.Before(task.SentAt.Add(VideoRelayConfirmationWindow)) &&
				now.Before(task.LastLookupAt.Add(VideoRelayTrackingLease)) {
				next = task.LastLookupAt.Add(VideoRelayTrackingLease)
			} else {
				next = now.Add(VideoRelayTrackingRetry)
			}
		}
	}
	if next.IsZero() {
		return nil
	}
	if next.Before(now) {
		next = now
	}
	return &next
}

func earlierVideoRelayTime(first, second time.Time) time.Time {
	if first.Before(second) {
		return first
	}
	return second
}
