package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
)

var ErrRegistrationIPBlocked = infraerrors.Forbidden("REGISTRATION_IP_BLOCKED", "Your current network cannot register new accounts. Existing accounts can still sign in. Contact the site administrator if you have questions.")

func invalidRegistrationIPBlacklist(message string) error {
	return infraerrors.BadRequest("REGISTRATION_IP_BLACKLIST_INVALID", message)
}

// NormalizeRegistrationIPBlacklist accepts individual addresses only, preserving input order.
func NormalizeRegistrationIPBlacklist(ips []string) ([]string, error) {
	normalized := make([]string, 0, len(ips))
	seen := make(map[string]struct{}, len(ips))
	for i, raw := range ips {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		parsed := net.ParseIP(raw)
		if parsed == nil {
			return nil, invalidRegistrationIPBlacklist(fmt.Sprintf("Entry %d must be a valid individual IPv4 or IPv6 address", i+1))
		}
		address := parsed.String()
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		normalized = append(normalized, address)
	}
	return normalized, nil
}

// ParseRegistrationIPBlacklist rejects malformed storage, including null array elements.
func ParseRegistrationIPBlacklist(raw string) ([]string, error) {
	var entries []*string
	if err := json.Unmarshal([]byte(raw), &entries); err != nil || entries == nil {
		return nil, invalidRegistrationIPBlacklist("ips must be an array of IP address strings")
	}
	ips := make([]string, 0, len(entries))
	for i, entry := range entries {
		if entry == nil {
			return nil, invalidRegistrationIPBlacklist(fmt.Sprintf("Entry %d must be an IP address string", i+1))
		}
		ips = append(ips, *entry)
	}
	return NormalizeRegistrationIPBlacklist(ips)
}

// GetRegistrationIPBlacklist differentiates an absent list from invalid stored data.
func (s *SettingService) GetRegistrationIPBlacklist(ctx context.Context) ([]string, error) {
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyRegistrationIPBlacklist)
	if errors.Is(err, ErrSettingNotFound) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get registration IP blacklist: %w", err)
	}
	ips, err := ParseRegistrationIPBlacklist(raw)
	if err != nil {
		return nil, nil
	}
	return ips, nil
}

// SetRegistrationIPBlacklist updates only the independent setting, without a read-back.
func (s *SettingService) SetRegistrationIPBlacklist(ctx context.Context, ips []string) ([]string, error) {
	normalized, err := NormalizeRegistrationIPBlacklist(ips)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("encode registration IP blacklist: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeyRegistrationIPBlacklist, string(data)); err != nil {
		return nil, fmt.Errorf("save registration IP blacklist: %w", err)
	}
	return normalized, nil
}

// GetRegistrationAccessSettings replaces the existing single registration-enabled query.
// A missing or invalid list adds no restriction; query failure retains the previous closed default.
func (s *SettingService) GetRegistrationAccessSettings(ctx context.Context) (bool, []string) {
	settings, err := s.settingRepo.GetMultiple(ctx, []string{SettingKeyRegistrationEnabled, SettingKeyRegistrationIPBlacklist})
	if err != nil {
		return false, nil
	}
	ips, _ := ParseRegistrationIPBlacklist(settings[SettingKeyRegistrationIPBlacklist])
	return settings[SettingKeyRegistrationEnabled] == "true", ips
}

// checkRegistrationAllowed is called only at the existing account-creation checks.
func (s *AuthService) checkRegistrationAllowed(ctx context.Context, signupSource string, allowDingTalkBypass bool) error {
	if s.settingService == nil {
		return ErrRegDisabled
	}
	enabled, blacklist := s.settingService.GetRegistrationAccessSettings(ctx)
	if !enabled && !(allowDingTalkBypass && s.canBypassRegistrationDisabledForOAuth(ctx, signupSource)) {
		return ErrRegDisabled
	}
	if len(blacklist) == 0 {
		return nil
	}
	binding := SessionBindingFromContext(ctx)
	if binding == nil {
		return nil
	}
	parsed := net.ParseIP(binding.IP)
	if parsed == nil {
		return nil
	}
	if allowed, _ := ip.CheckIPRestriction(parsed.String(), nil, blacklist); !allowed {
		return ErrRegistrationIPBlocked
	}
	return nil
}
