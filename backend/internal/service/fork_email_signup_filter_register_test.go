//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Copy values at both guarded creation entries: the original stub keeps pointers.
type forkSignupCreateSnapshot struct {
	Balance     float64
	Notes       string
	Concurrency int
}

type forkSignupUserRepo struct {
	*userRepoStub
	snapshots []forkSignupCreateSnapshot
}

func (r *forkSignupUserRepo) capture(user *User) {
	r.snapshots = append(r.snapshots, forkSignupCreateSnapshot{
		Balance: user.Balance, Notes: user.Notes, Concurrency: user.Concurrency,
	})
}

func (r *forkSignupUserRepo) CreateWithEmailAliasGuard(ctx context.Context, user *User) error {
	r.capture(user)
	return r.userRepoStub.CreateWithEmailAliasGuard(ctx, user)
}

func (r *forkSignupUserRepo) CreateWithEmailAliasGuardAndDomainLimit(ctx context.Context, user *User, domain string) error {
	r.capture(user)
	return r.userRepoStub.CreateWithEmailAliasGuardAndDomainLimit(ctx, user, domain)
}

func TestForkEmailSignupFilter_RED(t *testing.T) {
	start, end := "18:00", "19:00"
	if timezone.Now().Hour() >= 12 {
		start, end = "06:00", "07:00"
	}
	cases := []struct {
		name, email, cardEnabled, filter, note string
		concurrency                            int
	}{
		{"RED1_plus_before_create", "a+b@qq.com", "true", `{"enabled":true,"trusted_domains":["qq.com"],"time_window_enabled":false,"window_start":"09:00","window_end":"18:00"}`, "[注册风险:高] 含+别名", 7},
		{"RED2_global_fallback_filtered", "x@gmail.com", "false", `{"enabled":true,"trusted_domains":["qq.com"],"time_window_enabled":false,"window_start":"09:00","window_end":"18:00"}`, "", 2},
		{"RED3_outside_time", "x@qq.com", "true", fmt.Sprintf(`{"enabled":true,"trusted_domains":["qq.com"],"time_window_enabled":true,"window_start":%q,"window_end":%q}`, start, end), "", 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &forkSignupUserRepo{userRepoStub: &userRepoStub{nextID: 52}}
			assigner := &defaultSubscriptionAssignerStub{}
			svc := newAuthService(repo.userRepoStub, map[string]string{
				SettingKeyRegistrationEnabled:                 "true",
				SettingKeyAuthSourceDefaultEmailGrantOnSignup: tc.cardEnabled,
				SettingKeyAuthSourceDefaultEmailBalance:       "12.5",
				SettingKeyAuthSourceDefaultEmailConcurrency:   "7",
				SettingKeyAuthSourceDefaultEmailSubscriptions: `[{"group_id":11,"validity_days":30}]`,
				"fork_email_signup_filter":                    tc.filter,
			}, nil, nil)
			svc.userRepo = repo
			svc.defaultSubAssigner = assigner
			_, user, err := svc.Register(context.Background(), tc.email, "password")
			require.NoError(t, err)
			require.NotNil(t, user)
			require.Len(t, repo.snapshots, 1)
			actual := repo.snapshots[0]
			t.Logf("creation snapshot: balance=%v notes=%q concurrency=%d", actual.Balance, actual.Notes, actual.Concurrency)
			assert.Equal(t, float64(0), actual.Balance, "grant must be filtered before user creation")
			assert.Equal(t, tc.note, actual.Notes)
			assert.Equal(t, tc.concurrency, actual.Concurrency)
			if tc.cardEnabled == "true" {
				require.Len(t, assigner.calls, 1)
				assert.Equal(t, int64(11), assigner.calls[0].GroupID)
				assert.Equal(t, 30, assigner.calls[0].ValidityDays)
			}
		})
	}
}

type forkSignupReadErrorRepo struct {
	*settingRepoStub
	filterReads int
}

func (r *forkSignupReadErrorRepo) GetValue(ctx context.Context, key string) (string, error) {
	if key == "fork_email_signup_filter" {
		r.filterReads++
		return "", errors.New("isolated filter read failure")
	}
	return r.settingRepoStub.GetValue(ctx, key)
}

func forkSignupSettings(filter string) map[string]string {
	return map[string]string{
		SettingKeyRegistrationEnabled:                 "true",
		SettingKeyAuthSourceDefaultEmailGrantOnSignup: "true",
		SettingKeyAuthSourceDefaultEmailBalance:       "12.5",
		SettingKeyAuthSourceDefaultEmailConcurrency:   "7",
		SettingKeyAuthSourceDefaultEmailSubscriptions: `[{"group_id":11,"validity_days":30}]`,
		"fork_email_signup_filter":                    filter,
	}
}

func TestForkEmailSignupFilter_RegisterPreservesOtherGrants(t *testing.T) {
	oldNow := forkEmailSignupFilterNow
	forkEmailSignupFilterNow = func() time.Time { return time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { forkEmailSignupFilterNow = oldNow })
	const enabled = `{"enabled":true,"trusted_domains":["qq.com"],"time_window_enabled":false,"window_start":"09:00","window_end":"18:00"}`
	const timeEnabled = `{"enabled":true,"trusted_domains":["qq.com"],"time_window_enabled":true,"window_start":"09:00","window_end":"18:00"}`
	for _, tc := range []struct {
		name, email, filter, cardBalance, note string
		balance                                float64
		domainQuota, missing, readFailure      bool
	}{
		{name: "trusted", email: "x@qq.com", filter: enabled, balance: 12.5},
		{name: "inside_time_untrusted", email: "x@gmail.com", filter: timeEnabled, balance: 12.5},
		{name: "inside_time_plus", email: "a+b@gmail.com", filter: timeEnabled, note: "[注册风险:高] 含+别名"},
		{name: "domain_quota_path", email: "a+b@custom.example", filter: enabled, note: "[注册风险:高] 含+别名", domainQuota: true},
		{name: "zero_balance_still_marks", email: "a+b@qq.com", filter: enabled, cardBalance: "0", note: "[注册风险:高] 含+别名"},
		{name: "disabled", email: "a+b@qq.com", filter: `{"enabled":false}`, balance: 12.5},
		{name: "missing", email: "a+b@qq.com", missing: true, balance: 12.5},
		{name: "corrupt", email: "a+b@qq.com", filter: `{`, balance: 12.5},
		{name: "invalid_enabled", email: "a+b@qq.com", filter: `{"enabled":true,"window_start":"18:00","window_end":"09:00"}`, balance: 12.5},
		{name: "read_error", email: "a+b@qq.com", filter: enabled, balance: 12.5, readFailure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := forkSignupSettings(tc.filter)
			if tc.missing {
				delete(settings, "fork_email_signup_filter")
			}
			if tc.cardBalance != "" {
				settings[SettingKeyAuthSourceDefaultEmailBalance] = tc.cardBalance
			}
			if tc.domainQuota {
				settings[SettingKeyRegistrationEmailSuffixWhitelist] = `["@example.com"]`
				settings[SettingKeyRegistrationEmailDomainQuotaEnabled] = "true"
			}
			repo := &forkSignupUserRepo{userRepoStub: &userRepoStub{nextID: 52, domainCounts: map[string]int{}}}
			assigner := &defaultSubscriptionAssignerStub{}
			svc := newAuthService(repo.userRepoStub, settings, nil, nil)
			svc.userRepo = repo
			svc.defaultSubAssigner = assigner
			var failedRepo *forkSignupReadErrorRepo
			if tc.readFailure {
				failedRepo = &forkSignupReadErrorRepo{settingRepoStub: &settingRepoStub{values: settings}}
				svc.settingService.settingRepo = failedRepo
			}
			_, user, err := svc.Register(context.Background(), tc.email, "password")
			require.NoError(t, err)
			require.NotNil(t, user)
			require.Len(t, repo.snapshots, 1)
			require.Equal(t, tc.balance, repo.snapshots[0].Balance)
			require.Equal(t, tc.note, repo.snapshots[0].Notes)
			require.Equal(t, 7, repo.snapshots[0].Concurrency)
			require.Equal(t, tc.balance, user.Balance)
			require.Equal(t, tc.note, user.Notes)
			require.Len(t, assigner.calls, 1)
			require.Equal(t, int64(11), assigner.calls[0].GroupID)
			require.Equal(t, 30, assigner.calls[0].ValidityDays)
			if tc.domainQuota {
				require.Equal(t, 1, repo.domainLimitedCreates)
			}
			if tc.readFailure {
				require.Equal(t, 1, failedRepo.filterReads)
			}
		})
	}
}

func TestForkEmailSignupFilter_RegisterPlatformQuotasUnchanged(t *testing.T) {
	var baseline [][]UserPlatformQuotaRecord
	for _, enabled := range []bool{false, true} {
		settings := forkSignupSettings(fmt.Sprintf(`{"enabled":%t,"trusted_domains":["qq.com"]}`, enabled))
		settings[SettingKeyDefaultPlatformQuotas] = `{"openai":{"weekly":12.34}}`
		repo := &forkSignupUserRepo{userRepoStub: &userRepoStub{nextID: 77}}
		quotaRepo := &userPlatformQuotaRepoStub{}
		svc := newAuthService(repo.userRepoStub, settings, nil, quotaRepo)
		svc.userRepo = repo
		_, _, err := svc.Register(context.Background(), "a+b@qq.com", "password")
		require.NoError(t, err)
		require.Len(t, repo.snapshots, 1)
		require.Len(t, quotaRepo.bulkInsertCalls, 1)
		require.Len(t, quotaRepo.bulkInsertCalls[0], 1)
		if !enabled {
			require.Equal(t, 12.5, repo.snapshots[0].Balance)
			baseline = quotaRepo.bulkInsertCalls
		} else {
			require.Zero(t, repo.snapshots[0].Balance)
			require.Equal(t, baseline, quotaRepo.bulkInsertCalls)
		}
		require.NotNil(t, quotaRepo.bulkInsertCalls[0][0].WeeklyLimitUSD)
		require.Equal(t, 12.34, *quotaRepo.bulkInsertCalls[0][0].WeeklyLimitUSD)
	}
}

func TestForkEmailSignupFilter_RegisterEmailVerificationDoesNotChangeResult(t *testing.T) {
	for _, email := range []string{"y@gmail.com", "c+d@qq.com"} {
		for _, verify := range []bool{false, true} {
			settings := forkSignupSettings(`{"enabled":true,"trusted_domains":["qq.com"]}`)
			settings[SettingKeyEmailVerifyEnabled] = fmt.Sprint(verify)
			cache := &emailCacheStub{data: &VerificationCodeData{Code: "246810", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute)}}
			repo := &forkSignupUserRepo{userRepoStub: &userRepoStub{nextID: 52}}
			svc := newAuthService(repo.userRepoStub, settings, cache, nil)
			svc.userRepo = repo
			_, user, err := svc.RegisterWithVerification(context.Background(), email, "password", "246810", "", "", "")
			require.NoError(t, err)
			require.NotNil(t, user)
			require.Len(t, repo.snapshots, 1)
			require.Zero(t, repo.snapshots[0].Balance)
			if email == "c+d@qq.com" {
				require.Equal(t, "[注册风险:高] 含+别名", repo.snapshots[0].Notes)
			} else {
				require.Empty(t, repo.snapshots[0].Notes)
			}
		}
	}
}

func TestForkEmailSignupFilter_ThirdPartyUnchanged(t *testing.T) {
	repo := &userRepoStub{nextID: 88}
	svc := newEmailOAuthAutoAuthService(repo, map[string]string{
		SettingKeyRegistrationEnabled:                  "true",
		SettingKeyAuthSourceDefaultGitHubGrantOnSignup: "true",
		SettingKeyAuthSourceDefaultGitHubBalance:       "21.75",
		SettingKeyAuthSourceDefaultGitHubConcurrency:   "9",
		"fork_email_signup_filter":                     `{"enabled":true,"trusted_domains":["qq.com"]}`,
	}, nil)
	user, err := svc.createEmailOAuthUser(context.Background(), "a+b@gmail.com", "alias", "github", "", "")
	require.NoError(t, err)
	require.NotNil(t, user)
	require.Equal(t, 21.75, user.Balance)
	require.Equal(t, 9, user.Concurrency)
	require.Empty(t, user.Notes)
	require.Len(t, repo.created, 1)
}
