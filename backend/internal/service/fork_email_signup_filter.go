package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

const (
	SettingKeyForkEmailSignupFilter = "fork_email_signup_filter"
	ForkEmailSignupHighRiskNote     = "[注册风险:高] 含+别名"
	forkEmailSignupFilterMaxDomains = 500
)

var forkEmailSignupFilterNow = timezone.Now

// ForkEmailSignupFilterConfig only controls the existing email signup balance grant.
type ForkEmailSignupFilterConfig struct {
	Enabled           bool     `json:"enabled"`
	TrustedDomains    []string `json:"trusted_domains"`
	TimeWindowEnabled bool     `json:"time_window_enabled"`
	WindowStart       string   `json:"window_start"`
	WindowEnd         string   `json:"window_end"`
}

func defaultForkEmailSignupFilterConfig() ForkEmailSignupFilterConfig {
	return ForkEmailSignupFilterConfig{TrustedDomains: []string{}, WindowStart: "09:00", WindowEnd: "18:00"}
}

func normalizeForkEmailSignupFilterConfig(in ForkEmailSignupFilterConfig) (ForkEmailSignupFilterConfig, error) {
	domains := make([]string, 0, len(in.TrustedDomains))
	seen := make(map[string]bool, len(in.TrustedDomains))
	for _, raw := range in.TrustedDomains {
		domain := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(raw), "@"))
		if domain == "" {
			continue
		}
		if len(domain) > 253 || !strings.Contains(domain, ".") || strings.ContainsAny(domain, "@*/") || strings.IndexFunc(domain, unicode.IsSpace) >= 0 {
			return ForkEmailSignupFilterConfig{}, infraerrors.BadRequest("FORK_EMAIL_SIGNUP_FILTER_INVALID_DOMAIN", "Invalid trusted email domain").WithMetadata(map[string]string{"domain": domain})
		}
		if !seen[domain] {
			domains = append(domains, domain)
			seen[domain] = true
		}
	}
	if len(domains) > forkEmailSignupFilterMaxDomains {
		return ForkEmailSignupFilterConfig{}, infraerrors.BadRequest("FORK_EMAIL_SIGNUP_FILTER_TOO_MANY_DOMAINS", "At most 500 trusted email domains are allowed")
	}
	start, startOK := parseForkClock(in.WindowStart)
	end, endOK := parseForkClock(in.WindowEnd)
	if !startOK || !endOK {
		return ForkEmailSignupFilterConfig{}, infraerrors.BadRequest("FORK_EMAIL_SIGNUP_FILTER_INVALID_TIME", "Time must use HH:MM format")
	}
	if start >= end {
		return ForkEmailSignupFilterConfig{}, infraerrors.BadRequest("FORK_EMAIL_SIGNUP_FILTER_TIME_ORDER", "Start time must precede end time; overnight windows are not supported")
	}
	in.TrustedDomains = domains
	return in, nil
}

func parseForkClock(v string) (int, bool) {
	if len(v) != 5 || v[2] != ':' {
		return 0, false
	}
	for _, i := range []int{0, 1, 3, 4} {
		if v[i] < '0' || v[i] > '9' {
			return 0, false
		}
	}
	hour := int(v[0]-'0')*10 + int(v[1]-'0')
	minute := int(v[3]-'0')*10 + int(v[4]-'0')
	if hour > 23 || minute > 59 {
		return 0, false
	}
	return hour*60 + minute, true
}

// decideForkEmailSignupGrant expects an enabled, normalized configuration.
func decideForkEmailSignupGrant(cfg ForkEmailSignupFilterConfig, email string, now time.Time) (zeroBalance bool, note string) {
	domain := ""
	if at := strings.LastIndexByte(email, '@'); at >= 0 {
		if strings.Contains(email[:at], "+") {
			return true, ForkEmailSignupHighRiskNote
		}
		domain = strings.ToLower(email[at+1:])
	}
	if cfg.TimeWindowEnabled {
		start, _ := parseForkClock(cfg.WindowStart)
		end, _ := parseForkClock(cfg.WindowEnd)
		minute := now.Hour()*60 + now.Minute()
		return minute < start || minute >= end, ""
	}
	for _, trusted := range cfg.TrustedDomains {
		if domain == trusted {
			return false, ""
		}
	}
	return true, ""
}

func (s *SettingService) GetForkEmailSignupFilterConfig(ctx context.Context) (ForkEmailSignupFilterConfig, bool, error) {
	cfg := defaultForkEmailSignupFilterConfig()
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyForkEmailSignupFilter)
	if errors.Is(err, ErrSettingNotFound) {
		return cfg, false, nil
	}
	if err != nil {
		return cfg, false, err
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Invalid email signup filter JSON: %v", err)
		return defaultForkEmailSignupFilterConfig(), true, nil
	}
	if cfg.TrustedDomains == nil {
		cfg.TrustedDomains = []string{}
	}
	if !cfg.Enabled {
		return cfg, false, nil
	}
	cfg, err = normalizeForkEmailSignupFilterConfig(cfg)
	if err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Invalid email signup filter settings: %s", infraerrors.Reason(err))
		return defaultForkEmailSignupFilterConfig(), true, nil
	}
	return cfg, false, nil
}

func (s *SettingService) UpdateForkEmailSignupFilterConfig(ctx context.Context, in ForkEmailSignupFilterConfig) (ForkEmailSignupFilterConfig, error) {
	cfg, err := normalizeForkEmailSignupFilterConfig(in)
	if err != nil {
		return ForkEmailSignupFilterConfig{}, err
	}
	data, err := json.Marshal(cfg)
	if err == nil {
		err = s.settingRepo.Set(ctx, SettingKeyForkEmailSignupFilter, string(data))
	}
	if err != nil {
		return ForkEmailSignupFilterConfig{}, infraerrors.InternalServer("FORK_EMAIL_SIGNUP_FILTER_SAVE_FAILED", "Failed to write email signup filter settings").WithCause(err)
	}
	return cfg, nil
}

func (s *AuthService) applyForkEmailSignupFilter(ctx context.Context, email string, plan *signupGrantPlan) string {
	if s == nil || s.settingService == nil || plan == nil {
		return ""
	}
	cfg, invalid, err := s.settingService.GetForkEmailSignupFilterConfig(ctx)
	if err != nil {
		logger.LegacyPrintf("service.auth", "[Auth] Failed to read email signup filter settings: %v", err)
		return ""
	}
	if invalid || !cfg.Enabled {
		return ""
	}
	zeroBalance, note := decideForkEmailSignupGrant(cfg, email, forkEmailSignupFilterNow())
	if zeroBalance {
		plan.Balance = 0
	}
	return note
}
