//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

var checkinTestNow = time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)

func checkinConfigForTest() CheckinConfig {
	c := defaultCheckinConfig()
	c.Enabled = true
	return c
}

func checkinUserForTest(tier string, reserved bool) CheckinUser {
	u := CheckinUser{Email: "user@example.test", Status: StatusActive, CreatedAt: checkinTestNow.Add(-48 * time.Hour)}
	if reserved {
		u.Email = "checkin" + LinuxDoConnectSyntheticEmailDomain
	}
	switch tier {
	case "veteran":
		u.TotalRecharged = decimal.NewFromInt(20)
	case "new_funded":
		u.TotalRecharged = decimal.NewFromInt(1)
	}
	return u
}

// The expected decision is independent of the implementation. Conditions are ordered as in SPEC 2.3.
func TestCheckinRequirementMatrix(t *testing.T) {
	bools := []bool{false, true}
	cases := 0
	for _, enabled := range bools {
		for _, captcha := range bools {
			for _, email := range bools {
				for _, skip := range bools {
					for _, tier := range []string{"veteran", "new_funded", "new_unfunded"} {
						for _, reserved := range bools {
							for _, usable := range bools {
								cases++
								t.Run(fmt.Sprintf("%t/%t/%t/%t/%s/%t/%t", enabled, captcha, email, skip, tier, reserved, usable), func(t *testing.T) {
									c := checkinConfigForTest()
									c.Enabled = enabled
									c.NewcomerCaptcha = captcha
									c.EmailVerifyEnabled = email
									c.EmailVerifySkipReserved = skip
									wantsEmail := tier == "new_unfunded" && email
									wantStatus, wantCaptcha, wantEmail := "available", false, false
									switch {
									case !enabled:
										wantStatus = "disabled"
									case tier == "veteran":
									case wantsEmail && reserved && !skip:
										wantStatus = "bind_email"
									case captcha && !usable:
										wantStatus = "unavailable"
									default:
										wantCaptcha = captcha
										wantEmail = wantsEmail && !reserved
									}
									got := resolveRequirement(c, true, checkinUserForTest(tier, reserved), CheckinStats{TotalDays: 3}, checkinTestNow, usable)
									require.Equal(t, wantStatus, got.Status)
									require.Equal(t, wantCaptcha, got.NeedsCaptcha)
									require.Equal(t, wantEmail, got.NeedsEmail)
								})
							}
						}
					}
				}
			}
		}
	}
	require.Equal(t, 192, cases)
}

func TestCheckinTierAndEmail(t *testing.T) {
	c := checkinConfigForTest()
	c.EmailVerifyEnabled = true
	for _, tc := range []struct {
		name         string
		total, admin string
		credit       bool
		tier         string
		email        bool
	}{
		{"V1", "20", "0", false, "veteran", false},
		{"V2", "0", "20", true, "veteran", false},
		{"V3", "0", "15", true, "new_funded", false},
		{"V4", "0", "0", true, "new_funded", false},
		{"unfunded", "0", "0", false, "new_unfunded", true},
		{"V27 below", "19.99999999", "0", false, "new_funded", false},
		{"V27 equal", "19.99999999", "0.00000001", true, "veteran", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := checkinUserForTest("new_unfunded", false)
			u.TotalRecharged = decimal.RequireFromString(tc.total)
			s := CheckinStats{AdminNet: decimal.RequireFromString(tc.admin), AdminHasCredit: tc.credit, TotalDays: 10}
			r := resolveRequirement(c, true, u, s, checkinTestNow, true)
			require.Equal(t, tc.tier, r.Tier)
			require.Equal(t, tc.email, r.NeedsEmail)
			require.Equal(t, tc.tier != "veteran", r.NeedsCaptcha)
		})
	}
	u := checkinUserForTest("new_unfunded", false)
	for signed := int64(0); signed <= 12; signed++ {
		stats := CheckinStats{TotalDays: signed}
		if signed >= 4 {
			stats.LastEmailDate = "2026-11-01"
			stats.SinceEmail = signed - 4
		}
		got := resolveRequirement(c, true, u, stats, checkinTestNow, true)
		require.Equal(t, signed == 3 || signed >= 11, got.NeedsEmail, "next signup after %d", signed)
	}
	c.EmailVerifyEvery = 0
	require.False(t, resolveRequirement(c, true, u, CheckinStats{TotalDays: 40, LastEmailDate: "2026-11-01", SinceEmail: 36}, checkinTestNow, true).NeedsEmail)
	c.EmailVerifyEnabled = false
	require.False(t, resolveRequirement(c, true, u, CheckinStats{TotalDays: 100}, checkinTestNow, true).NeedsEmail)
}

func TestCheckinConfigBoundaries(t *testing.T) {
	c := checkinConfigForTest()
	c.EmailVerifyEnabled = true
	u := checkinUserForTest("new_unfunded", false)
	for _, tc := range []struct {
		total string
		tier  string
	}{{"0", "new_unfunded"}, {"20", "veteran"}, {"21", "veteran"}} {
		u.TotalRecharged = decimal.RequireFromString(tc.total)
		require.Equal(t, tc.tier, resolveRequirement(c, true, u, CheckinStats{}, checkinTestNow, true).Tier)
	}
	u.TotalRecharged = decimal.Zero
	for _, n := range []int64{0, 2, 3, 4} {
		require.Equal(t, n >= 3, resolveRequirement(c, true, u, CheckinStats{TotalDays: n}, checkinTestNow, true).NeedsEmail)
	}
	for _, n := range []int64{0, 6, 7, 8} {
		require.Equal(t, n >= 7, resolveRequirement(c, true, u, CheckinStats{LastEmailDate: "2026-11-01", SinceEmail: n}, checkinTestNow, true).NeedsEmail)
	}
	for _, d := range []time.Duration{0, 24*time.Hour - time.Second, 24 * time.Hour, 48 * time.Hour} {
		u.CreatedAt = checkinTestNow.Add(-d)
		got := resolveRequirement(c, true, u, CheckinStats{}, checkinTestNow, true)
		if d < 24*time.Hour {
			require.Equal(t, "too_new", got.Status)
			require.Equal(t, u.CreatedAt.Add(24*time.Hour), got.AvailableAt)
			require.False(t, got.NeedsCaptcha)
			require.False(t, got.NeedsEmail)
		} else {
			require.Equal(t, "available", got.Status)
		}
	}
	for _, total := range []int64{0, 4, 5, 6} {
		low, high := int64(1), int64(5)
		if total >= 5 {
			low, high = 1, 1
		}
		require.Equal(t, low, checkinRewardCents(c, "new_unfunded", total, func(int64) int64 { return 0 }))
		require.Equal(t, high, checkinRewardCents(c, "new_unfunded", total, func(n int64) int64 { return n - 1 }))
	}
	for _, n := range []int64{0, 7, 29, 57} {
		c.VeteranMin = n
		c.VeteranMax = n
		c.NewcomerMin = n
		c.NewcomerMax = n
		require.Equal(t, n, checkinRewardCents(c, "veteran", 0, func(n int64) int64 { return n - 1 }))
		require.Equal(t, n, checkinRewardCents(c, "new_funded", 0, func(n int64) int64 { return n - 1 }))
	}
}

func TestCheckinConfigEachFieldEffective(t *testing.T) {
	type effect func(*testing.T)
	base := checkinConfigForTest()
	base.EmailVerifyEnabled = true
	u := checkinUserForTest("new_unfunded", false)
	stats := CheckinStats{TotalDays: 3}
	requirement := func(c CheckinConfig, u CheckinUser, s CheckinStats) CheckinRequirement {
		return resolveRequirement(c, true, u, s, checkinTestNow, true)
	}
	reward := func(c CheckinConfig, tier string, total int64, high bool) int64 {
		return checkinRewardCents(c, tier, total, func(n int64) int64 {
			if high {
				return n - 1
			}
			return 0
		})
	}
	cases := map[string]effect{
		"enabled": func(t *testing.T) {
			c := base
			c.Enabled = false
			require.Equal(t, "available", requirement(base, u, stats).Status)
			require.Equal(t, "disabled", requirement(c, u, stats).Status)
		},
		"veteran_threshold": func(t *testing.T) {
			c := base
			c.VeteranThreshold = decimal.Zero
			require.Equal(t, "new_unfunded", requirement(base, u, stats).Tier)
			require.Equal(t, "veteran", requirement(c, u, stats).Tier)
		},
		"veteran_min": func(t *testing.T) {
			c := base
			c.VeteranMin = 6
			require.Equal(t, int64(5), reward(base, "veteran", 0, false))
			require.Equal(t, int64(6), reward(c, "veteran", 0, false))
		},
		"veteran_max": func(t *testing.T) {
			c := base
			c.VeteranMax = 31
			require.Equal(t, int64(30), reward(base, "veteran", 0, true))
			require.Equal(t, int64(31), reward(c, "veteran", 0, true))
		},
		"newcomer_min": func(t *testing.T) {
			c := base
			c.NewcomerMin = 2
			require.Equal(t, int64(1), reward(base, "new_funded", 0, false))
			require.Equal(t, int64(2), reward(c, "new_funded", 0, false))
		},
		"newcomer_max": func(t *testing.T) {
			c := base
			c.NewcomerMax = 6
			require.Equal(t, int64(5), reward(base, "new_funded", 0, true))
			require.Equal(t, int64(6), reward(c, "new_funded", 0, true))
		},
		"newcomer_cap": func(t *testing.T) {
			c := base
			c.NewcomerCap = 6
			require.Equal(t, int64(1), reward(base, "new_unfunded", 5, true))
			require.Equal(t, int64(5), reward(c, "new_unfunded", 5, true))
		},
		"newcomer_after_cap": func(t *testing.T) {
			c := base
			c.NewcomerAfterCap = 2
			require.Equal(t, int64(1), reward(base, "new_unfunded", 5, true))
			require.Equal(t, int64(2), reward(c, "new_unfunded", 5, true))
		},
		"newcomer_captcha": func(t *testing.T) {
			c := base
			c.NewcomerCaptcha = false
			require.True(t, requirement(base, u, stats).NeedsCaptcha)
			require.False(t, requirement(c, u, stats).NeedsCaptcha)
		},
		"min_account_age_days": func(t *testing.T) {
			c := base
			c.MinAccountAgeDays = 3
			require.Equal(t, "available", requirement(base, u, stats).Status)
			require.Equal(t, "too_new", requirement(c, u, stats).Status)
		},
		"ip_daily_limit": func(t *testing.T) {
			s, f := newCheckinTestFixture(t)
			f.ipCount = 3
			f.setConfig(t, base)
			_, err := s.Checkin(f.ctx, 1, "", CheckinProof{Token: "fixture"}, "")
			require.Equal(t, "CHECKIN_IP_LIMIT", infraerrors.Reason(err))
			c := base
			c.IPDailyLimit = 0
			f.setConfig(t, c)
			_, err = s.Checkin(f.ctx, 1, "", CheckinProof{Token: "fixture"}, "")
			require.NoError(t, err)
			require.Equal(t, 1, f.ipLocks)
		},
		"email_verify_enabled": func(t *testing.T) {
			c := base
			c.EmailVerifyEnabled = false
			require.True(t, requirement(base, u, stats).NeedsEmail)
			require.False(t, requirement(c, u, stats).NeedsEmail)
		},
		"email_verify_first": func(t *testing.T) {
			c := base
			c.EmailVerifyFirst = 4
			require.True(t, requirement(base, u, stats).NeedsEmail)
			require.False(t, requirement(c, u, stats).NeedsEmail)
		},
		"email_verify_every": func(t *testing.T) {
			c := base
			c.EmailVerifyEvery = 8
			s := CheckinStats{LastEmailDate: "2026-11-01", SinceEmail: 7}
			require.True(t, requirement(base, u, s).NeedsEmail)
			require.False(t, requirement(c, u, s).NeedsEmail)
		},
		"email_verify_skip_reserved": func(t *testing.T) {
			c := base
			c.EmailVerifySkipReserved = false
			v := checkinUserForTest("new_unfunded", true)
			require.Equal(t, "available", requirement(base, v, stats).Status)
			require.Equal(t, "bind_email", requirement(c, v, stats).Status)
		},
	}
	typ := reflect.TypeOf(base)
	require.Len(t, cases, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		key := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		test, ok := cases[key]
		require.True(t, ok, "missing config coverage %s", key)
		t.Run(key, test)
	}
}

func TestCheckinConfigValidate(t *testing.T) {
	s, f := newCheckinTestFixture(t)
	baseline := checkinConfigForTest()
	raw, err := json.Marshal(baseline)
	require.NoError(t, err)
	settings := f.setting
	before := settings.values[SettingKeyCheckinConfig]
	invalid := map[string]string{
		"enabled": "null", "veteran_threshold": "1000000.01", "veteran_min": "0.301", "veteran_max": "0.01", "newcomer_min": "0.06", "newcomer_max": "101", "newcomer_cap": "-0.01", "newcomer_after_cap": "0.001", "newcomer_captcha": "1", "min_account_age_days": "3651", "ip_daily_limit": "1001", "email_verify_enabled": "1", "email_verify_first": "0", "email_verify_every": "-1", "email_verify_skip_reserved": "null",
	}
	for key, bad := range invalid {
		for _, missing := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/missing=%t", key, missing), func(t *testing.T) {
				var fields map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(raw, &fields))
				if missing {
					delete(fields, key)
				} else {
					fields[key] = json.RawMessage(bad)
				}
				input, e := json.Marshal(fields)
				require.NoError(t, e)
				_, e = s.SaveConfig(context.Background(), input)
				require.Equal(t, "CHECKIN_CONFIG_INVALID", infraerrors.Reason(e))
				require.Contains(t, infraerrors.FromError(e).Metadata, "field")
				require.Equal(t, before, settings.values[SettingKeyCheckinConfig])
				require.Zero(t, settings.writes)
			})
		}
	}
	baseline.NewcomerMin = 7
	baseline.NewcomerMax = 7
	baseline.VeteranMin = 29
	baseline.VeteranMax = 57
	raw, err = json.Marshal(baseline)
	require.NoError(t, err)
	saved, err := s.SaveConfig(context.Background(), raw)
	require.NoError(t, err)
	require.Equal(t, baseline, saved)
	got, err := s.GetConfig(context.Background())
	require.NoError(t, err)
	require.True(t, got.ConfigValid)
	require.Equal(t, baseline, got.Config)
	require.Equal(t, 1, settings.writes)
}

// Narrow doubles observe the required transaction, calls and credential consumption without real external IO.
type checkinSettingFake struct {
	SettingRepository
	values        map[string]string
	reads, writes int
	valueReads    int
}

func (f *checkinSettingFake) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	f.reads++
	out := map[string]string{}
	for _, k := range keys {
		if v, ok := f.values[k]; ok {
			out[k] = v
		}
	}
	return out, nil
}
func (f *checkinSettingFake) GetValue(_ context.Context, key string) (string, error) {
	f.valueReads++
	if value, ok := f.values[key]; ok {
		return value, nil
	}
	return "", ErrSettingNotFound
}
func (f *checkinSettingFake) Set(_ context.Context, k, v string) error {
	f.writes++
	f.values[k] = v
	return nil
}

type checkinEmailFake struct {
	calls                           int
	sent                            int
	sendEmail, sendSite, sendLocale string
	lastEmail                       string
	err                             error
}

func (f *checkinEmailFake) VerifyCode(_ context.Context, email, code string) error {
	f.calls++
	f.lastEmail = email
	return f.err
}
func (f *checkinEmailFake) SendVerifyCode(_ context.Context, email, site string, locale ...string) error {
	f.sent++
	f.sendEmail = email
	f.sendSite = site
	if len(locale) > 0 {
		f.sendLocale = locale[0]
	}
	return f.err
}

type checkinTestRepo struct {
	CheckinRepository
	ctx                  context.Context
	setting              *checkinSettingFake
	email                *checkinEmailFake
	user                 CheckinUser
	stats                CheckinStats
	lockedConfig         map[string]string
	ipCount, ipLocks     int
	attempts, commits    int
	record               CheckinRecord
	verifyCalls          int
	verifyErr            error
	verifiedProvider     string
	sequence             []string
	commitErr            error
	transactionCommitErr error
	stateLoads           int
	lastLoadMonth        string
}

func (f *checkinTestRepo) WithTx(ctx context.Context, fn func(context.Context) error) error {
	f.sequence = append(f.sequence, "begin")
	e := fn(ctx)
	if e == nil {
		f.commits++
		f.sequence = append(f.sequence, "commit")
		return f.transactionCommitErr
	}
	return e
}
func (f *checkinTestRepo) LockUser(_ context.Context, _ int64, _ []string) (CheckinUser, error) {
	f.sequence = append(f.sequence, "lock_user")
	u := f.user
	u.Settings = f.setting.values
	if f.lockedConfig != nil {
		u.Settings = f.lockedConfig
	}
	return u, nil
}
func (f *checkinTestRepo) LoadStats(_ context.Context, _ int64, _ string) (CheckinStats, error) {
	f.sequence = append(f.sequence, "stats")
	return f.stats, nil
}
func (f *checkinTestRepo) LoadState(_ context.Context, _ int64, _ []string, _, month string) (CheckinState, error) {
	f.stateLoads++
	f.lastLoadMonth = month
	u := f.user
	u.Settings = f.setting.values
	return CheckinState{User: u, Stats: f.stats}, nil
}
func (f *checkinTestRepo) LockIP(_ context.Context, _, _ string) error {
	f.ipLocks++
	f.sequence = append(f.sequence, "lock_ip")
	return nil
}
func (f *checkinTestRepo) Commit(_ context.Context, r CheckinRecord, limit int) (CheckinCommitResult, error) {
	f.attempts++
	f.sequence = append(f.sequence, "write")
	if f.commitErr != nil {
		return CheckinCommitResult{}, f.commitErr
	}
	if limit > 0 && f.ipCount >= limit {
		return CheckinCommitResult{IPCount: f.ipCount}, nil
	}
	f.record = r
	balance := decimal.NewFromInt(10).Add(decimal.New(r.AmountCents, -2))
	return CheckinCommitResult{ID: 1, CreatedAt: r.CreatedAt, Balance: &balance}, nil
}
func (f *checkinTestRepo) setConfig(t *testing.T, c CheckinConfig) {
	t.Helper()
	raw, e := json.Marshal(c)
	require.NoError(t, e)
	f.setting.values[SettingKeyCheckinConfig] = string(raw)
}
func newCheckinTestFixture(t *testing.T) (*CheckinService, *checkinTestRepo) {
	f := &checkinTestRepo{ctx: WithSessionBinding(context.Background(), &SessionBinding{IP: "2001:0db8:0:0:0:0:0:1"}), user: checkinUserForTest("new_unfunded", false), setting: &checkinSettingFake{values: map[string]string{SettingKeyTurnstileSiteKey: "fixture-site", SettingKeyTurnstileSecretKey: "fixture-only"}}, email: &checkinEmailFake{}}
	f.setConfig(t, checkinConfigForTest())
	s := &CheckinService{repo: f, settings: f.setting, settingService: &SettingService{settingRepo: f.setting}, email: f.email, Clock: func() time.Time { return checkinTestNow }, Random: func(n int64) int64 { return n - 1 }, verifyCaptcha: func(_ context.Context, c checkinCaptchaSelection, _ CheckinProof, _ string) error {
		f.verifyCalls++
		f.verifiedProvider = c.Provider
		f.sequence = append(f.sequence, "verify_captcha")
		return f.verifyErr
	}}
	return s, f
}

func TestCheckinSubmitAndCredentialPriority(t *testing.T) {
	t.Run("V1 veteran", func(t *testing.T) {
		s, f := newCheckinTestFixture(t)
		f.user = checkinUserForTest("veteran", false)
		got, e := s.Checkin(f.ctx, 1, "", CheckinProof{}, "")
		require.NoError(t, e)
		require.Equal(t, 0.30, got.Amount)
		require.Equal(t, 10.30, got.Balance)
		require.Equal(t, "veteran", f.record.Tier)
		require.Equal(t, "none", f.record.Verification)
		require.Equal(t, []string{"begin", "lock_user", "stats", "write", "commit"}, f.sequence)
		require.Zero(t, f.setting.reads)
	})
	t.Run("V8 both required", func(t *testing.T) {
		s, f := newCheckinTestFixture(t)
		c := checkinConfigForTest()
		c.EmailVerifyEnabled = true
		f.setConfig(t, c)
		f.stats.TotalDays = 3
		_, e := s.Checkin(f.ctx, 1, "", CheckinProof{}, "123456")
		require.Equal(t, "CHECKIN_CAPTCHA_REQUIRED", infraerrors.Reason(e))
		require.Zero(t, f.email.calls)
		_, e = s.Checkin(f.ctx, 1, "", CheckinProof{Token: "fixture"}, "")
		require.Equal(t, "CHECKIN_EMAIL_REQUIRED", infraerrors.Reason(e))
		require.Zero(t, f.commits)
		_, e = s.Checkin(f.ctx, 1, "different@example.test", CheckinProof{Token: "fixture"}, "123456")
		require.NoError(t, e)
		require.Equal(t, 1, f.email.calls)
		require.Equal(t, f.user.Email, f.email.lastEmail)
		require.Equal(t, "email", f.record.Verification)
		require.Equal(t, "2001:db8::1", f.record.IP)
	})
	t.Run("V18 disabled global switch", func(t *testing.T) {
		s, f := newCheckinTestFixture(t)
		f.setting.values[SettingKeyTurnstileEnabled] = "false"
		got, e := s.Checkin(f.ctx, 1, "", CheckinProof{Token: "fixture"}, "")
		require.NoError(t, e)
		require.Equal(t, 0.05, got.Amount)
		require.Equal(t, "turnstile", f.verifiedProvider)
		require.Equal(t, 1, f.setting.reads)
	})
	t.Run("V19 incomplete provider", func(t *testing.T) {
		s, f := newCheckinTestFixture(t)
		f.setting.values[SettingKeyTencentCaptchaEnabled] = "true"
		state, e := s.GetState(f.ctx, 1, "")
		require.NoError(t, e)
		require.Equal(t, "unavailable", state.Status)
		require.False(t, state.NeedsCaptcha)
		_, e = s.Checkin(f.ctx, 1, "", CheckinProof{}, "")
		require.Equal(t, "CHECKIN_UNAVAILABLE", infraerrors.Reason(e))
		f.user = checkinUserForTest("veteran", false)
		_, e = s.Checkin(f.ctx, 1, "", CheckinProof{}, "")
		require.NoError(t, e)
	})
	t.Run("V31 done dominates bad proof", func(t *testing.T) {
		s, f := newCheckinTestFixture(t)
		n := int64(1)
		f.stats.TodayCents = &n
		f.verifyErr = errors.New("mock failure")
		_, e := s.Checkin(f.ctx, 1, "", CheckinProof{Token: "fixture"}, "")
		require.Equal(t, "CHECKIN_ALREADY_DONE", infraerrors.Reason(e))
		require.Equal(t, 1, f.verifyCalls)
		require.Zero(t, f.attempts)
	})
	t.Run("V31 disabled dominates proof", func(t *testing.T) {
		s, f := newCheckinTestFixture(t)
		c := checkinConfigForTest()
		c.Enabled = false
		f.setConfig(t, c)
		_, e := s.Checkin(f.ctx, 1, "", CheckinProof{Token: "fixture"}, "")
		require.Equal(t, "CHECKIN_DISABLED", infraerrors.Reason(e))
		require.Zero(t, f.verifyCalls)
		require.Empty(t, f.sequence)
	})
	t.Run("V31 unused email remains", func(t *testing.T) {
		s, f := newCheckinTestFixture(t)
		c := checkinConfigForTest()
		c.EmailVerifyEnabled = true
		f.setConfig(t, c)
		f.stats.TotalDays = 2
		_, e := s.Checkin(f.ctx, 1, "", CheckinProof{Token: "fixture"}, "123456")
		require.NoError(t, e)
		require.Zero(t, f.email.calls)
		f.stats.TotalDays = 3
		_, e = s.Checkin(f.ctx, 1, "", CheckinProof{Token: "fixture"}, "123456")
		require.NoError(t, e)
		require.Equal(t, 1, f.email.calls)
	})
	t.Run("V31 changed public identity", func(t *testing.T) {
		s, f := newCheckinTestFixture(t)
		f.lockedConfig = map[string]string{}
		for k, v := range f.setting.values {
			f.lockedConfig[k] = v
		}
		f.lockedConfig[SettingKeyTurnstileSiteKey] = "new-site"
		_, e := s.Checkin(f.ctx, 1, "", CheckinProof{Token: "fixture"}, "")
		require.Equal(t, "CHECKIN_CAPTCHA_REQUIRED", infraerrors.Reason(e))
		require.Equal(t, 1, f.verifyCalls)
		require.Zero(t, f.attempts)
	})
	t.Run("V22 invalid config", func(t *testing.T) {
		s, f := newCheckinTestFixture(t)
		f.setting.values[SettingKeyCheckinConfig] = `{"enabled":false}`
		state, e := s.GetState(f.ctx, 1, "")
		require.NoError(t, e)
		require.Equal(t, "unavailable", state.Status)
		require.True(t, state.Enabled)
		b, e := json.Marshal(state)
		require.NoError(t, e)
		require.JSONEq(t, `{"enabled":true,"status":"unavailable"}`, string(b))
		_, e = s.Checkin(f.ctx, 1, "", CheckinProof{}, "")
		require.Equal(t, "CHECKIN_UNAVAILABLE", infraerrors.Reason(e))
	})
	t.Run("IP boundary", func(t *testing.T) {
		for _, n := range []int{0, 2, 3, 4} {
			s, f := newCheckinTestFixture(t)
			f.ipCount = n
			_, e := s.Checkin(f.ctx, 1, "", CheckinProof{Token: "fixture"}, "")
			if n >= 3 {
				require.Equal(t, "CHECKIN_IP_LIMIT", infraerrors.Reason(e))
			} else {
				require.NoError(t, e)
			}
			require.Equal(t, 1, f.ipLocks)
		}
	})
}

func TestCheckinEmailSender(t *testing.T) {
	for _, status := range []string{"disabled", "unavailable", "done", "too_new", "bind_email", "available_without_email", "available_with_email"} {
		t.Run(status, func(t *testing.T) {
			s, f := newCheckinTestFixture(t)
			c := checkinConfigForTest()
			c.EmailVerifyEnabled = true
			f.stats.TotalDays = 3
			switch status {
			case "disabled":
				c.Enabled = false
			case "done":
				n := int64(1)
				f.stats.TodayCents = &n
			case "too_new":
				f.user.CreatedAt = checkinTestNow
			case "bind_email":
				c.EmailVerifySkipReserved = false
				f.user.Email = "fixture" + LinuxDoConnectSyntheticEmailDomain
			case "available_without_email":
				c.EmailVerifyEnabled = false
			case "unavailable":
				f.setting.values[SettingKeyTurnstileSiteKey] = ""
			}
			f.setConfig(t, c)
			got, e := s.SendEmailCode(f.ctx, 1, "zh-CN")
			require.Equal(t, 1, f.stateLoads)
			require.Empty(t, f.lastLoadMonth)
			require.Zero(t, f.setting.reads)
			if status == "available_with_email" {
				require.NoError(t, e)
				require.Equal(t, "u***@example.test", got.MaskedEmail)
				require.Equal(t, 60, got.CooldownSeconds)
				require.Equal(t, 1, f.email.sent)
				require.Equal(t, f.user.Email, f.email.sendEmail)
				require.Equal(t, "Sub2API", f.email.sendSite)
				require.Equal(t, "zh-CN", f.email.sendLocale)
				require.Equal(t, 1, f.setting.valueReads)
			} else {
				require.Equal(t, "CHECKIN_EMAIL_NOT_REQUIRED", infraerrors.Reason(e))
				require.Zero(t, f.email.sent)
				require.Zero(t, f.setting.valueReads)
			}
		})
	}
}

type checkinTurnstileVerifier struct{ calls int }

func (v *checkinTurnstileVerifier) VerifyToken(context.Context, string, string, string) (*TurnstileVerifyResponse, error) {
	v.calls++
	return &TurnstileVerifyResponse{Success: true}, nil
}
func TestCheckinTurnstileUsesExistingVerifier(t *testing.T) {
	_, f := newCheckinTestFixture(t)
	f.setting.values[SettingKeyTurnstileEnabled] = "false"
	settingService := &SettingService{settingRepo: f.setting}
	v := &checkinTurnstileVerifier{}
	s := NewCheckinService(f, f.setting, settingService, NewTurnstileService(settingService, v), nil, nil, nil, nil, nil)
	s.Clock = func() time.Time { return checkinTestNow }
	s.Random = func(int64) int64 { return 0 }
	_, e := s.Checkin(f.ctx, 1, "", CheckinProof{Token: "fixture"}, "")
	require.NoError(t, e)
	require.Equal(t, 1, v.calls)
	require.Equal(t, 1, f.setting.reads)
	// The same disabled global setting must still leave the existing login and
	// registration captcha entry points off, despite the configured keys.
	oldAuth := newAuthServiceForCaptchaTest(f.setting.values, false, v, nil)
	require.NoError(t, oldAuth.VerifyCaptcha(f.ctx, CaptchaProof{}, "2001:db8::1"))
	require.NoError(t, oldAuth.VerifyCaptchaForRegister(f.ctx, CaptchaProof{}, "2001:db8::1", ""))
	require.Equal(t, 1, v.calls)
}

func TestCheckinCaptchaSelection(t *testing.T) {
	for _, provider := range []string{"turnstile", "tencent", "aliyun", "none", "conflict", "incomplete_tencent", "incomplete_aliyun"} {
		t.Run(provider, func(t *testing.T) {
			s, f := newCheckinTestFixture(t)
			v := f.setting.values
			switch provider {
			case "tencent":
				v[SettingKeyTencentCaptchaEnabled] = "true"
				v[SettingKeyTencentCaptchaAppID] = "123"
				v[SettingKeyTencentCaptchaAppSecretKey] = "fixture-only"
				v[SettingKeyTencentCaptchaCloudSecretID] = "fixture-only"
				v[SettingKeyTencentCaptchaCloudSecretKey] = "fixture-only"
			case "aliyun", "incomplete_aliyun":
				v[SettingKeyAliyunCaptchaEnabled] = "true"
				v[SettingKeyAliyunCaptchaAccessKeyID] = "fixture-only"
				v[SettingKeyAliyunCaptchaAccessKeySecret] = "fixture-only"
				v[SettingKeyAliyunCaptchaSceneID] = "fixture-scene"
				if provider == "aliyun" {
					v[SettingKeyAliyunCaptchaPrefix] = "fixture-prefix"
				}
			case "none":
				v[SettingKeyTurnstileSiteKey] = ""
			case "conflict":
				v[SettingKeyTurnstileEnabled] = "true"
				v[SettingKeyTencentCaptchaEnabled] = "true"
			case "incomplete_tencent":
				v[SettingKeyTencentCaptchaEnabled] = "true"
			}
			got, e := s.GetState(f.ctx, 1, "")
			require.NoError(t, e)
			require.Equal(t, 1, f.stateLoads)
			require.Zero(t, f.setting.reads)
			if provider == "turnstile" || provider == "tencent" || provider == "aliyun" {
				require.Equal(t, "available", got.Status)
				require.True(t, got.NeedsCaptcha)
				require.Equal(t, provider, got.CaptchaProvider)
				raw, e := json.Marshal(got)
				require.NoError(t, e)
				require.NotContains(t, string(raw), "fixture-only")
			} else {
				require.Equal(t, "unavailable", got.Status)
				require.False(t, got.NeedsCaptcha)
			}
		})
	}
}

func TestCheckinChangedCaptchaProviderRejectsSuccessfulOldProof(t *testing.T) {
	s, f := newCheckinTestFixture(t)
	f.lockedConfig = map[string]string{}
	for k, v := range f.setting.values {
		f.lockedConfig[k] = v
	}
	f.lockedConfig[SettingKeyTencentCaptchaEnabled] = "true"
	f.lockedConfig[SettingKeyTencentCaptchaAppID] = "123"
	f.lockedConfig[SettingKeyTencentCaptchaAppSecretKey] = "fixture-only"
	f.lockedConfig[SettingKeyTencentCaptchaCloudSecretID] = "fixture-only"
	f.lockedConfig[SettingKeyTencentCaptchaCloudSecretKey] = "fixture-only"
	_, e := s.Checkin(f.ctx, 1, "", CheckinProof{Token: "fixture"}, "")
	require.Equal(t, "CHECKIN_CAPTCHA_REQUIRED", infraerrors.Reason(e))
	require.Equal(t, 1, f.verifyCalls)
	require.Zero(t, f.attempts)
}

func TestCheckinAmbiguousTransactionCommit(t *testing.T) {
	t.Run("transaction response lost", func(t *testing.T) {
		s, f := newCheckinTestFixture(t)
		f.user = checkinUserForTest("veteran", false)
		f.transactionCommitErr = errors.New("connection lost during COMMIT")
		got, err := s.Checkin(f.ctx, 1, "", CheckinProof{}, "")
		require.Equal(t, "CHECKIN_FAILED", infraerrors.Reason(err))
		var appErr *infraerrors.ApplicationError
		require.ErrorAs(t, err, &appErr)
		require.Equal(t, "true", appErr.Metadata["result_unknown"])
		require.NotContains(t, appErr.Message, "余额未变动")
		require.Zero(t, got)
		require.Equal(t, 1, f.commits)
		require.Equal(t, 1, f.attempts)
	})
	t.Run("write failed before transaction commit", func(t *testing.T) {
		s, f := newCheckinTestFixture(t)
		f.user = checkinUserForTest("veteran", false)
		f.commitErr = errors.New("write failed")
		_, err := s.Checkin(f.ctx, 1, "", CheckinProof{}, "")
		require.Equal(t, "CHECKIN_FAILED", infraerrors.Reason(err))
		var appErr *infraerrors.ApplicationError
		require.ErrorAs(t, err, &appErr)
		require.Empty(t, appErr.Metadata["result_unknown"])
		require.Contains(t, appErr.Message, "余额未变动")
		require.Zero(t, f.commits)
	})
}
