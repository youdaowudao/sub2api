//go:build unit

package service

import (
	"context"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

// Exercise registration through the existing entry point: a blocked mailbox
// cannot be created, while eligible registrations retain the fork's grant rules.
func TestRegistrationBlacklistAndForkSignupFilter(t *testing.T) {
	for _, tc := range []struct {
		name, email string
		blocked     bool
		domainQuota bool
		balance     float64
		note        string
	}{
		{name: "blocked_trusted_mailbox", email: "blocked@qq.com", blocked: true},
		{name: "blocked_gmail_alias", email: "SpamUser+one@googlemail.com", blocked: true},
		{name: "blocked_alias_before_domain_quota", email: "SpamUser+two@googlemail.com", blocked: true, domainQuota: true},
		{name: "allowed_trusted_keeps_balance", email: "allowed@qq.com", balance: 12.5},
		{name: "allowed_untrusted_only_filters_balance", email: "allowed@outlook.com"},
		{name: "allowed_plus_keeps_risk_note", email: "allowed+tag@qq.com", note: ForkEmailSignupHighRiskNote},
		{name: "allowed_domain_quota_keeps_other_grants", email: "allowed@custom.example", domainQuota: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := forkSignupSettings(`{"enabled":true,"trusted_domains":["qq.com"],"time_window_enabled":false}`)
			// Keep the persisted key literal so this regression also compiles on
			// the pre-upgrade baseline, before the official constant exists.
			settings["registration_email_blacklist"] = `["spam.user@gmail.com","blocked@qq.com"]`
			if tc.domainQuota {
				settings[SettingKeyRegistrationEmailSuffixWhitelist] = `["@qq.com"]`
				settings[SettingKeyRegistrationEmailDomainQuotaEnabled] = "true"
			}
			repo := &forkSignupUserRepo{userRepoStub: &userRepoStub{nextID: 73, domainCounts: map[string]int{}}}
			assigner := &defaultSubscriptionAssignerStub{}
			svc := newAuthService(repo.userRepoStub, settings, nil, nil)
			svc.userRepo = repo
			svc.defaultSubAssigner = assigner

			_, user, err := svc.Register(context.Background(), tc.email, "regression-password")
			if tc.blocked {
				require.Error(t, err)
				require.Equal(t, "EMAIL_BLOCKED", infraerrors.FromError(err).Reason)
				require.Nil(t, user)
				require.Empty(t, repo.created)
				require.Empty(t, repo.snapshots)
				require.Empty(t, assigner.calls)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, user)
			require.Len(t, repo.snapshots, 1)
			require.Equal(t, tc.balance, user.Balance)
			require.Equal(t, tc.balance, repo.snapshots[0].Balance)
			require.Equal(t, tc.note, user.Notes)
			require.Equal(t, tc.note, repo.snapshots[0].Notes)
			require.Equal(t, 7, user.Concurrency)
			require.Len(t, assigner.calls, 1)
			require.Equal(t, int64(11), assigner.calls[0].GroupID)
			require.Equal(t, 30, assigner.calls[0].ValidityDays)
			if tc.domainQuota {
				require.Equal(t, 1, repo.domainLimitedCreates)
			}
		})
	}
}
