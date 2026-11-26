package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
)

const SettingKeyCheckinConfig = "checkin_config"

// Reward values are integral cents internally. The threshold retains decimal
// precision because existing recharge and administrator adjustment data do.
type CheckinConfig struct {
	Enabled                 bool            `json:"enabled"`
	VeteranThreshold        decimal.Decimal `json:"veteran_threshold"`
	VeteranMin              int64           `json:"veteran_min"`
	VeteranMax              int64           `json:"veteran_max"`
	NewcomerMin             int64           `json:"newcomer_min"`
	NewcomerMax             int64           `json:"newcomer_max"`
	NewcomerCap             int64           `json:"newcomer_cap"`
	NewcomerAfterCap        int64           `json:"newcomer_after_cap"`
	NewcomerCaptcha         bool            `json:"newcomer_captcha"`
	MinAccountAgeDays       int             `json:"min_account_age_days"`
	IPDailyLimit            int             `json:"ip_daily_limit"`
	EmailVerifyEnabled      bool            `json:"email_verify_enabled"`
	EmailVerifyFirst        int             `json:"email_verify_first"`
	EmailVerifyEvery        int             `json:"email_verify_every"`
	EmailVerifySkipReserved bool            `json:"email_verify_skip_reserved"`
}

func defaultCheckinConfig() CheckinConfig {
	return CheckinConfig{VeteranThreshold: decimal.NewFromInt(20), VeteranMin: 5, VeteranMax: 30, NewcomerMin: 1, NewcomerMax: 5, NewcomerCap: 5, NewcomerAfterCap: 1, NewcomerCaptcha: true, MinAccountAgeDays: 1, IPDailyLimit: 3, EmailVerifyFirst: 3, EmailVerifyEvery: 7, EmailVerifySkipReserved: true}
}

func checkinCentsString(cents int64) string  { return decimal.New(cents, -2).StringFixed(2) }
func checkinCentsAmount(cents int64) float64 { return decimal.New(cents, -2).InexactFloat64() }

// MarshalJSON writes currency amounts as decimal JSON numbers, not cents or strings.
func (c CheckinConfig) MarshalJSON() ([]byte, error) {
	amount := func(cents int64) json.Number { return json.Number(checkinCentsString(cents)) }
	return json.Marshal(map[string]any{
		"enabled": c.Enabled, "veteran_threshold": json.Number(c.VeteranThreshold.StringFixed(2)),
		"veteran_min": amount(c.VeteranMin), "veteran_max": amount(c.VeteranMax),
		"newcomer_min": amount(c.NewcomerMin), "newcomer_max": amount(c.NewcomerMax), "newcomer_cap": amount(c.NewcomerCap), "newcomer_after_cap": amount(c.NewcomerAfterCap),
		"newcomer_captcha": c.NewcomerCaptcha, "min_account_age_days": c.MinAccountAgeDays, "ip_daily_limit": c.IPDailyLimit,
		"email_verify_enabled": c.EmailVerifyEnabled, "email_verify_first": c.EmailVerifyFirst, "email_verify_every": c.EmailVerifyEvery, "email_verify_skip_reserved": c.EmailVerifySkipReserved,
	})
}

var checkinAmountPattern = regexp.MustCompile(`^\d+(\.\d{1,2})?$`)
var checkinIntegerPattern = regexp.MustCompile(`^\d+$`)

func checkinConfigInvalid(field, reason string) error {
	return infraerrors.BadRequest("CHECKIN_CONFIG_INVALID", fmt.Sprintf("%s: %s", field, reason)).WithMetadata(map[string]string{"field": field})
}

func readCheckinConfig(values map[string]string) (CheckinConfig, error) {
	raw, ok := values[SettingKeyCheckinConfig]
	if !ok {
		return defaultCheckinConfig(), nil
	}
	return parseCheckinConfig([]byte(raw))
}

func parseCheckinConfig(raw []byte) (CheckinConfig, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return CheckinConfig{}, checkinConfigInvalid("config", "must be a JSON object")
	}
	var c CheckinConfig
	boolean := func(key string, dst *bool) error {
		b := bytes.TrimSpace(fields[key])
		switch string(b) {
		case "true":
			*dst = true
		case "false":
			*dst = false
		default:
			return checkinConfigInvalid(key, "must be a boolean")
		}
		return nil
	}
	amount := func(key string, max int64) (decimal.Decimal, error) {
		b := bytes.TrimSpace(fields[key])
		if !checkinAmountPattern.Match(b) {
			return decimal.Zero, checkinConfigInvalid(key, "must be a nonnegative number with at most two decimal places")
		}
		var number json.Number
		if err := json.Unmarshal(b, &number); err != nil {
			return decimal.Zero, checkinConfigInvalid(key, "must be a JSON number")
		}
		n, err := decimal.NewFromString(number.String())
		if err != nil || n.GreaterThan(decimal.NewFromInt(max)) {
			return decimal.Zero, checkinConfigInvalid(key, fmt.Sprintf("must be between 0 and %d", max))
		}
		return decimal.RequireFromString(n.String()), nil
	}
	integer := func(key string, dst *int, min, max int) error {
		b := bytes.TrimSpace(fields[key])
		if !checkinIntegerPattern.Match(b) {
			return checkinConfigInvalid(key, "must be an integer")
		}
		n, e := strconv.ParseInt(string(b), 10, 64)
		if e != nil || n < int64(min) || n > int64(max) {
			return checkinConfigInvalid(key, fmt.Sprintf("must be between %d and %d", min, max))
		}
		*dst = int(n)
		return nil
	}
	for _, f := range []struct {
		k string
		v *bool
	}{{"enabled", &c.Enabled}, {"newcomer_captcha", &c.NewcomerCaptcha}, {"email_verify_enabled", &c.EmailVerifyEnabled}, {"email_verify_skip_reserved", &c.EmailVerifySkipReserved}} {
		if e := boolean(f.k, f.v); e != nil {
			return c, e
		}
	}
	var err error
	c.VeteranThreshold, err = amount("veteran_threshold", 1000000)
	if err != nil {
		return c, err
	}
	for _, f := range []struct {
		k string
		v *int64
	}{{"veteran_min", &c.VeteranMin}, {"veteran_max", &c.VeteranMax}, {"newcomer_min", &c.NewcomerMin}, {"newcomer_max", &c.NewcomerMax}, {"newcomer_cap", &c.NewcomerCap}, {"newcomer_after_cap", &c.NewcomerAfterCap}} {
		n, e := amount(f.k, 100)
		if e != nil {
			return c, e
		}
		*f.v = n.Shift(2).IntPart()
	}
	for _, f := range []struct {
		k        string
		v        *int
		min, max int
	}{{"min_account_age_days", &c.MinAccountAgeDays, 0, 3650}, {"ip_daily_limit", &c.IPDailyLimit, 0, 1000}, {"email_verify_first", &c.EmailVerifyFirst, 1, 3650}, {"email_verify_every", &c.EmailVerifyEvery, 0, 3650}} {
		if e := integer(f.k, f.v, f.min, f.max); e != nil {
			return c, e
		}
	}
	if c.VeteranMin > c.VeteranMax {
		return c, checkinConfigInvalid("veteran_min", "must not exceed veteran_max")
	}
	if c.NewcomerMin > c.NewcomerMax {
		return c, checkinConfigInvalid("newcomer_min", "must not exceed newcomer_max")
	}
	if len(fields) != 15 {
		return c, checkinConfigInvalid("config", "must contain exactly the supported configuration fields")
	}
	return c, nil
}
