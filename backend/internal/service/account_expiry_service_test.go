package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type scheduleTimerExpiryRepo struct {
	AccountRepository
	account      Account
	pauseErr     error
	resumeCalled bool
}

func (r *scheduleTimerExpiryRepo) AutoPauseExpiredAccounts(context.Context, time.Time) (int64, error) {
	return 0, r.pauseErr
}

func (r *scheduleTimerExpiryRepo) ResumeDueScheduleTimers(context.Context) (int64, error) {
	r.resumeCalled = true
	r.account.Schedulable = true
	return 1, nil
}

func TestAccountExpiryServiceResumesScheduleTimer(t *testing.T) {
	for _, test := range []struct {
		name     string
		pauseErr error
	}{
		{name: "normal"}, {name: "existing expiry check fails", pauseErr: errors.New("old expiry check failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &scheduleTimerExpiryRepo{account: Account{ID: 17, Schedulable: false}, pauseErr: test.pauseErr}
			NewAccountExpiryService(repo, time.Minute).runOnce()
			require.True(t, repo.resumeCalled, "due timers must run even when the existing expiry check fails")
			require.True(t, repo.account.Schedulable, "the background check must execute the backend enable operation")
		})
	}
}
