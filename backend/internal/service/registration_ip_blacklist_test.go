//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestRegistrationIPBlacklistCreationEntrypoints(t *testing.T) {
	const email = "new@example.com"
	ctx := WithSessionBinding(context.Background(), &SessionBinding{IP: "192.0.2.10"})
	for _, entry := range []struct {
		name string
		run  func(*AuthService, context.Context) (*User, error)
	}{
		{"email", func(s *AuthService, c context.Context) (*User, error) {
			_, u, e := s.Register(c, email, "password")
			return u, e
		}},
		{"legacy", func(s *AuthService, c context.Context) (*User, error) {
			_, u, e := s.LoginOrRegisterOAuth(c, email, "new")
			return u, e
		}},
		{"linuxdo", func(s *AuthService, c context.Context) (*User, error) {
			_, u, e := s.LoginOrRegisterOAuthWithTokenPair(c, email, "new", "", "", "linuxdo")
			return u, e
		}},
		{"wechat", func(s *AuthService, c context.Context) (*User, error) {
			_, u, e := s.LoginOrRegisterOAuthWithTokenPair(c, email, "new", "", "", "wechat")
			return u, e
		}},
		{"oidc", func(s *AuthService, c context.Context) (*User, error) {
			_, u, e := s.LoginOrRegisterOAuthWithTokenPair(c, email, "new", "", "", "oidc")
			return u, e
		}},
		{"dingtalk", func(s *AuthService, c context.Context) (*User, error) {
			_, u, e := s.LoginOrRegisterOAuthWithTokenPair(c, email, "new", "", "", "dingtalk")
			return u, e
		}},
		{"pending_email", func(s *AuthService, c context.Context) (*User, error) {
			_, u, e := s.RegisterOAuthEmailAccount(c, email, "password", "123456", "", "dingtalk")
			return u, e
		}},
		{"verified_email", func(s *AuthService, c context.Context) (*User, error) {
			_, u, e := s.RegisterVerifiedOAuthEmailAccount(c, email, "password", "", "google")
			return u, e
		}},
		{"auto_google", func(s *AuthService, c context.Context) (*User, error) {
			return s.createEmailOAuthUser(c, email, "new", "google", "", "")
		}},
		{"auto_github", func(s *AuthService, c context.Context) (*User, error) {
			return s.createEmailOAuthUser(c, email, "new", "github", "", "")
		}},
		{"auto_oidc", func(s *AuthService, c context.Context) (*User, error) {
			return s.createEmailOAuthUser(c, email, "new", "oidc", "", "")
		}},
	} {
		t.Run(entry.name, func(t *testing.T) {
			repo := &userRepoStub{nextID: 1}
			quotas := &userPlatformQuotaRepoStub{}
			svc := newAuthService(repo, map[string]string{SettingKeyRegistrationEnabled: "true", "registration_ip_blacklist": `["192.0.2.10"]`}, &emailCacheStub{data: &VerificationCodeData{Code: "123456", ExpiresAt: time.Now().Add(time.Hour)}}, quotas)
			svc.refreshTokenCache = &refreshTokenCacheStub{}
			user, err := entry.run(svc, ctx)
			require.Equal(t, "REGISTRATION_IP_BLOCKED", infraerrors.Reason(err))
			require.Nil(t, user)
			require.Empty(t, repo.created)
			require.Empty(t, quotas.bulkInsertCalls)
		})
	}
}

func TestRegistrationIPBlacklistPreservesOldRegistrationRules(t *testing.T) {
	for _, tc := range []struct {
		name, raw, ip, enabled string
		blocked                bool
		disabled               bool
	}{
		{"absent", "", "192.0.2.10", "true", false, false},
		{"empty", `[]`, "192.0.2.10", "true", false, false},
		{"different", `["192.0.2.11"]`, "192.0.2.10", "true", false, false},
		{"nil_element", `["192.0.2.10",null]`, "192.0.2.10", "true", false, false},
		{"bad_element", `["192.0.2.10","bad"]`, "192.0.2.10", "true", false, false},
		{"root_null", `null`, "192.0.2.10", "true", false, false},
		{"unknown", `["192.0.2.10"]`, "unknown", "true", false, false},
		{"no_ip", `["192.0.2.10"]`, "", "true", false, false},
		{"ipv4_mapped", `["192.0.2.10"]`, "::ffff:192.0.2.10", "true", true, false},
		{"ipv6_equivalent", `["2001:db8::1"]`, "2001:0db8:0:0::1", "true", true, false},
		{"disabled", `["192.0.2.10"]`, "192.0.2.10", "false", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := map[string]string{SettingKeyRegistrationEnabled: tc.enabled}
			if tc.raw != "" {
				settings[SettingKeyRegistrationIPBlacklist] = tc.raw
			}
			repo := &userRepoStub{nextID: 1}
			svc := newAuthService(repo, settings, nil, nil)
			ctx := context.Background()
			if tc.ip != "" {
				ctx = WithSessionBinding(ctx, &SessionBinding{IP: tc.ip})
			}
			token, user, err := svc.Register(ctx, "normal@example.com", "password")
			switch {
			case tc.blocked:
				require.ErrorIs(t, err, ErrRegistrationIPBlocked)
				require.Empty(t, token)
				require.Nil(t, user)
				require.Empty(t, repo.created)
			case tc.disabled:
				require.ErrorIs(t, err, ErrRegDisabled)
				require.Empty(t, repo.created)
			default:
				require.NoError(t, err)
				require.NotEmpty(t, token)
				require.NotNil(t, user)
				require.Len(t, repo.created, 1)
			}
		})
	}
}

func TestRegistrationIPBlacklistSettingsAndRemoval(t *testing.T) {
	ctx := WithSessionBinding(context.Background(), &SessionBinding{IP: "192.0.2.10"})
	repo := newWritableSettingRepoStub(map[string]string{SettingKeyRegistrationEnabled: "true", "unrelated": "keep"})
	svc := newAuthService(&userRepoStub{nextID: 1}, map[string]string{}, nil, nil)
	svc.settingService = NewSettingService(repo, svc.cfg)
	list, err := svc.settingService.GetRegistrationIPBlacklist(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{}, list)
	list, err = svc.settingService.SetRegistrationIPBlacklist(ctx, []string{" 192.0.2.10 ", "::ffff:192.0.2.10", "2001:0db8::1", ""})
	require.NoError(t, err)
	require.Equal(t, []string{"192.0.2.10", "2001:db8::1"}, list)
	require.Equal(t, 1, repo.setCalls)
	require.Equal(t, 1, repo.getValueCalls)
	require.Equal(t, "keep", repo.values["unrelated"])
	before := repo.getMultipleCalls
	require.ErrorIs(t, svc.checkRegistrationAllowed(ctx, "", false), ErrRegistrationIPBlocked)
	require.Equal(t, before+1, repo.getMultipleCalls)
	require.Equal(t, 1, repo.getValueCalls)
	list, err = svc.settingService.SetRegistrationIPBlacklist(ctx, []string{})
	require.NoError(t, err)
	require.Equal(t, []string{}, list)
	require.NoError(t, svc.checkRegistrationAllowed(ctx, "", false))
	require.Equal(t, "[]", repo.values[SettingKeyRegistrationIPBlacklist])
	repo.values[SettingKeyRegistrationIPBlacklist] = `["192.0.2.10",null]`
	list, err = svc.settingService.GetRegistrationIPBlacklist(ctx)
	require.NoError(t, err)
	require.Nil(t, list)
	require.NoError(t, svc.checkRegistrationAllowed(ctx, "", false))
}

func TestRegistrationIPBlacklistExistingAccount(t *testing.T) {
	ctx := WithSessionBinding(context.Background(), &SessionBinding{IP: "192.0.2.10"})
	userRepo := &userRepoStub{nextID: 1}
	svc := newAuthService(userRepo, map[string]string{SettingKeyRegistrationEnabled: "true", SettingKeyRegistrationIPBlacklist: `["192.0.2.10"]`}, nil, nil)
	svc.refreshTokenCache = &refreshTokenCacheStub{}
	hash, err := svc.HashPassword("password")
	require.NoError(t, err)
	existing := &User{ID: 1, Email: "old@example.com", PasswordHash: hash, Status: StatusActive, Role: RoleUser}
	userRepo.user = existing
	settings := svc.settingService.settingRepo.(*settingRepoStub)
	_, u, err := svc.Login(ctx, existing.Email, "password")
	require.NoError(t, err)
	require.Equal(t, existing.ID, u.ID)
	before := settings.getMultipleCalls
	_, u, err = svc.LoginOrRegisterOAuth(ctx, existing.Email, "old")
	require.NoError(t, err)
	require.Equal(t, existing.ID, u.ID)
	for _, source := range []string{"linuxdo", "wechat", "oidc", "dingtalk"} {
		tokens, u, err := svc.LoginOrRegisterOAuthWithTokenPair(ctx, existing.Email, "old", "", "", source)
		require.NoError(t, err)
		require.NotNil(t, tokens)
		require.Equal(t, existing.ID, u.ID)
	}
	require.Empty(t, userRepo.created)
	require.Equal(t, before, settings.getMultipleCalls, "existing OAuth accounts must not query registration access")
}

func TestRegistrationIPBlacklistDingTalkBypass(t *testing.T) {
	cfg := minDingTalkURLs()
	svc := newAuthServiceWithDingTalkCfg(map[string]string{
		SettingKeyRegistrationEnabled:                  "false",
		SettingKeyDingTalkConnectEnabled:               "true",
		SettingKeyDingTalkConnectBypassRegistration:    "true",
		SettingKeyDingTalkConnectCorpRestrictionPolicy: "internal_only",
		SettingKeyRegistrationIPBlacklist:              `["192.0.2.10"]`,
	}, cfg)
	ctx := WithSessionBinding(context.Background(), &SessionBinding{IP: "192.0.2.10"})
	require.ErrorIs(t, svc.checkRegistrationAllowed(ctx, "dingtalk", true), ErrRegistrationIPBlocked)
	require.NoError(t, svc.checkRegistrationAllowed(context.Background(), "dingtalk", true))
	require.ErrorIs(t, svc.checkRegistrationAllowed(context.Background(), "linuxdo", true), ErrRegDisabled)
	require.ErrorIs(t, svc.checkRegistrationAllowed(context.Background(), "dingtalk", false), ErrRegDisabled)
}

// A failed registration settings query retains the old closed decision and still
// makes the separately configured enterprise OAuth bypass decision.
type registrationIPAccessFailureRepo struct{ *settingRepoStub }

func (r *registrationIPAccessFailureRepo) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	if len(keys) == 2 && keys[0] == SettingKeyRegistrationEnabled {
		return nil, errors.New("registration settings unavailable")
	}
	return r.settingRepoStub.GetMultiple(ctx, keys)
}

func TestRegistrationIPBlacklistRegistrationQueryFailure(t *testing.T) {
	svc := newAuthServiceWithDingTalkCfg(map[string]string{
		SettingKeyRegistrationEnabled:                  "true",
		SettingKeyDingTalkConnectEnabled:               "true",
		SettingKeyDingTalkConnectBypassRegistration:    "true",
		SettingKeyDingTalkConnectCorpRestrictionPolicy: "internal_only",
	}, minDingTalkURLs())
	svc.settingService.settingRepo = &registrationIPAccessFailureRepo{svc.settingService.settingRepo.(*settingRepoStub)}
	require.ErrorIs(t, svc.checkRegistrationAllowed(context.Background(), "", false), ErrRegDisabled)
	require.NoError(t, svc.checkRegistrationAllowed(context.Background(), "dingtalk", true))
	require.ErrorIs(t, svc.checkRegistrationAllowed(context.Background(), "oidc", true), ErrRegDisabled)
}

func TestRegistrationIPBlacklistBeforeInvitationAndTokens(t *testing.T) {
	svc := newAuthService(&userRepoStub{}, map[string]string{
		SettingKeyRegistrationEnabled: "true", SettingKeyInvitationCodeEnabled: "true",
		SettingKeyRegistrationIPBlacklist: `["192.0.2.10"]`,
	}, nil, nil)
	svc.refreshTokenCache = &refreshTokenCacheStub{}
	ctx := WithSessionBinding(context.Background(), &SessionBinding{IP: "192.0.2.10"})
	token, user, err := svc.RegisterWithVerification(ctx, "fixture@example.com", "password", "", "", "invite", "")
	require.ErrorIs(t, err, ErrRegistrationIPBlocked)
	require.Empty(t, token)
	require.Nil(t, user)
	pair, user, err := svc.LoginOrRegisterOAuthWithTokenPair(ctx, "fixture@example.com", "fixture", "invite", "", "linuxdo")
	require.ErrorIs(t, err, ErrRegistrationIPBlocked)
	require.Nil(t, pair)
	require.Nil(t, user)
}
