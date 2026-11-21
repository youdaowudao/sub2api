package dto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestScheduleTimerIsManagementOnly(t *testing.T) {
	until := time.Now().Add(6 * time.Hour)
	account := &service.Account{ID: 17, Status: service.StatusActive, Schedulable: true, ScheduleResumeAt: &until}
	raw, err := json.Marshal(account)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "ScheduleResumeAt", "default scheduler/plugin JSON must not carry timer data")
	require.NotContains(t, string(raw), "schedule_resume_at")
	full := AccountFromServiceShallow(account)
	require.Equal(t, &until, full.ScheduleResumeAt)
	list := AccountListItemFromAccount(full)
	require.Equal(t, &until, list.ScheduleResumeAt)
	raw, err = json.Marshal(list)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"schedule_resume_at":`)
	require.True(t, account.IsSchedulable(), "only the original switch and original conditions govern selection")
	account.Schedulable = false
	require.False(t, account.IsSchedulable())
	account.Schedulable = true
	account.TempUnschedulableUntil = &until
	require.False(t, account.IsSchedulable(), "enabling does not clear existing system protection")
}
