//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type forkFilterSettingRepo struct {
	*settingRepoStub
	writes   map[string]string
	writeErr error
}

func (r *forkFilterSettingRepo) Set(_ context.Context, key, value string) error {
	if r.writeErr != nil {
		return r.writeErr
	}
	if r.writes == nil {
		r.writes = make(map[string]string)
	}
	r.writes[key] = value
	return nil
}

func TestForkEmailSignupFilter_Normalize(t *testing.T) {
	cfg := defaultForkEmailSignupFilterConfig()
	cfg.TrustedDomains = []string{"  @QQ.COM  ", "", "qq.com", "Gmail.Com", "\t", "@"}
	got, err := normalizeForkEmailSignupFilterConfig(cfg)
	require.NoError(t, err)
	require.Equal(t, []string{"qq.com", "gmail.com"}, got.TrustedDomains)
	cfg.TrustedDomains = nil
	got, err = normalizeForkEmailSignupFilterConfig(cfg)
	require.NoError(t, err)
	require.Equal(t, []string{}, got.TrustedDomains)
	for _, domain := range []string{"foo@bar.com", "bad domain.com", "bad\tdomain.com", "bad\u2003domain.com", "*.qq.com", "qq.com/path", "localhost", strings.Repeat("x", 250) + ".com"} {
		t.Run(domain, func(t *testing.T) {
			cfg.TrustedDomains = []string{domain}
			_, err := normalizeForkEmailSignupFilterConfig(cfg)
			require.Equal(t, "FORK_EMAIL_SIGNUP_FILTER_INVALID_DOMAIN", infraerrors.Reason(err))
			require.Equal(t, domain, infraerrors.FromError(err).Metadata["domain"])
		})
	}
	cfg.TrustedDomains = make([]string, 501)
	for i := range cfg.TrustedDomains {
		cfg.TrustedDomains[i] = fmt.Sprintf("d%d.example", i)
	}
	_, err = normalizeForkEmailSignupFilterConfig(cfg)
	require.Equal(t, "FORK_EMAIL_SIGNUP_FILTER_TOO_MANY_DOMAINS", infraerrors.Reason(err))
	cfg.TrustedDomains = cfg.TrustedDomains[:500]
	_, err = normalizeForkEmailSignupFilterConfig(cfg)
	require.NoError(t, err)
	for _, pair := range [][2]string{{"18:00", "09:00"}, {"09:00", "09:00"}, {"23:00", "01:00"}} {
		cfg.WindowStart, cfg.WindowEnd = pair[0], pair[1]
		_, err = normalizeForkEmailSignupFilterConfig(cfg)
		require.Equal(t, "FORK_EMAIL_SIGNUP_FILTER_TIME_ORDER", infraerrors.Reason(err))
	}
	cfg.WindowStart, cfg.WindowEnd = "9:00", "18:00"
	_, err = normalizeForkEmailSignupFilterConfig(cfg)
	require.Equal(t, "FORK_EMAIL_SIGNUP_FILTER_INVALID_TIME", infraerrors.Reason(err))
}

func TestForkEmailSignupFilter_ParseClock(t *testing.T) {
	for _, tc := range []struct {
		value  string
		minute int
	}{{"00:00", 0}, {"09:00", 540}, {"23:59", 1439}} {
		minute, ok := parseForkClock(tc.value)
		require.True(t, ok)
		require.Equal(t, tc.minute, minute)
	}
	for _, value := range []string{"9:00", "09:0", "24:00", "12:60", "-1:00", "09:00 ", " 09:00", "09-00", "aa:bb", ""} {
		_, ok := parseForkClock(value)
		require.False(t, ok, value)
	}
}

func TestForkEmailSignupFilter_Decision(t *testing.T) {
	cfg := defaultForkEmailSignupFilterConfig()
	cfg.Enabled = true
	cfg.TrustedDomains = []string{"qq.com"}
	now := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	for _, window := range []bool{false, true} {
		cfg.TimeWindowEnabled = window
		for _, email := range []string{"a+b@qq.com", "a+b@gmail.com", "+a@qq.com"} {
			zero, note := decideForkEmailSignupGrant(cfg, email, now)
			require.True(t, zero, email)
			require.Equal(t, "[注册风险:高] 含+别名", note)
		}
	}
	cfg.TimeWindowEnabled = true
	for _, tc := range []struct {
		hour, minute int
		zero         bool
	}{{8, 59, true}, {9, 0, false}, {17, 59, false}, {18, 0, true}, {23, 59, true}} {
		for _, email := range []string{"x@qq.com", "x@gmail.com"} {
			zero, note := decideForkEmailSignupGrant(cfg, email, time.Date(2026, 10, 24, tc.hour, tc.minute, 0, 0, time.UTC))
			require.Equal(t, tc.zero, zero, "%s %02d:%02d", email, tc.hour, tc.minute)
			require.Empty(t, note)
		}
	}
	cfg.TimeWindowEnabled = false
	for _, tc := range []struct {
		email string
		zero  bool
	}{{"x@qq.com", false}, {"a.b@qq.com", false}, {"x@QQ.COM", false}, {"x@gmail.com", true}, {"x@vip.qq.com", true}, {"without-at", true}} {
		zero, note := decideForkEmailSignupGrant(cfg, tc.email, now)
		require.Equal(t, tc.zero, zero, tc.email)
		require.Empty(t, note)
	}
	cfg.TrustedDomains = []string{}
	zero, note := decideForkEmailSignupGrant(cfg, "x@qq.com", now)
	require.True(t, zero)
	require.Empty(t, note)
}

func TestForkEmailSignupFilter_Read(t *testing.T) {
	for _, tc := range []struct {
		name, raw        string
		missing, invalid bool
		want             ForkEmailSignupFilterConfig
	}{
		{"missing", "", true, false, defaultForkEmailSignupFilterConfig()},
		{"missing_times", `{"enabled":true,"trusted_domains":["@QQ.COM"]}`, false, false, ForkEmailSignupFilterConfig{Enabled: true, TrustedDomains: []string{"qq.com"}, WindowStart: "09:00", WindowEnd: "18:00"}},
		{"null_disabled", `{"trusted_domains":null}`, false, false, defaultForkEmailSignupFilterConfig()},
		{"null_enabled", `{"enabled":true,"trusted_domains":null}`, false, false, ForkEmailSignupFilterConfig{Enabled: true, TrustedDomains: []string{}, WindowStart: "09:00", WindowEnd: "18:00"}},
		{"broken", `{`, false, true, defaultForkEmailSignupFilterConfig()},
		{"disabled_invalid_content", `{"enabled":false,"trusted_domains":["bad"],"window_start":"bad"}`, false, false, ForkEmailSignupFilterConfig{TrustedDomains: []string{"bad"}, WindowStart: "bad", WindowEnd: "18:00"}},
		{"enabled_invalid_content", `{"enabled":true,"window_start":"18:00","window_end":"09:00"}`, false, true, defaultForkEmailSignupFilterConfig()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{}
			if !tc.missing {
				values[SettingKeyForkEmailSignupFilter] = tc.raw
			}
			svc := NewSettingService(&settingRepoStub{values: values}, nil)
			got, invalid, err := svc.GetForkEmailSignupFilterConfig(context.Background())
			require.NoError(t, err)
			require.Equal(t, tc.invalid, invalid)
			require.Equal(t, tc.want, got)
		})
	}
	readErr := errors.New("isolated settings read failed")
	svc := NewSettingService(&settingRepoStub{err: readErr}, nil)
	_, _, err := svc.GetForkEmailSignupFilterConfig(context.Background())
	require.ErrorIs(t, err, readErr)
}

func TestForkEmailSignupFilter_Save(t *testing.T) {
	repo := &forkFilterSettingRepo{settingRepoStub: &settingRepoStub{}}
	svc := NewSettingService(repo, nil)
	cfg := defaultForkEmailSignupFilterConfig()
	cfg.TrustedDomains = []string{"  @QQ.COM ", "qq.com"}
	got, err := svc.UpdateForkEmailSignupFilterConfig(context.Background(), cfg)
	require.NoError(t, err)
	require.Equal(t, []string{"qq.com"}, got.TrustedDomains)
	require.Len(t, repo.writes, 1)
	var stored ForkEmailSignupFilterConfig
	require.NoError(t, json.Unmarshal([]byte(repo.writes[SettingKeyForkEmailSignupFilter]), &stored))
	require.Equal(t, got, stored)
	repo.writes = nil
	cfg.WindowStart = "20:00"
	_, err = svc.UpdateForkEmailSignupFilterConfig(context.Background(), cfg)
	require.Equal(t, "FORK_EMAIL_SIGNUP_FILTER_TIME_ORDER", infraerrors.Reason(err))
	require.Empty(t, repo.writes, "invalid disabled settings must not be written")
	cfg.WindowStart = "09:00"
	repo.writeErr = errors.New("isolated settings write failed")
	_, err = svc.UpdateForkEmailSignupFilterConfig(context.Background(), cfg)
	require.Equal(t, "FORK_EMAIL_SIGNUP_FILTER_SAVE_FAILED", infraerrors.Reason(err))
}

func TestForkEmailSignupFilter_NilDependenciesKeepPlan(t *testing.T) {
	plan := signupGrantPlan{Balance: 12.5, Concurrency: 7}
	var absent *AuthService
	require.Empty(t, absent.applyForkEmailSignupFilter(context.Background(), "a+b@qq.com", &plan))
	require.Empty(t, (&AuthService{}).applyForkEmailSignupFilter(context.Background(), "a+b@qq.com", &plan))
	require.Empty(t, (&AuthService{}).applyForkEmailSignupFilter(context.Background(), "a+b@qq.com", nil))
	require.Equal(t, 12.5, plan.Balance)
	require.Equal(t, 7, plan.Concurrency)
}
