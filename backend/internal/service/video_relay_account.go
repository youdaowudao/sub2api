package service

import (
	"encoding/json"
	"math"
	"slices"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const OpenAIVideoRelayCredentialsKey = "openai_videos"

type OpenAIVideoRelaySettings struct {
	Resolutions    []string `json:"resolutions"`
	MinSeconds     int      `json:"min_seconds"`
	MaxSeconds     int      `json:"max_seconds"`
	DefaultSeconds int      `json:"default_seconds"`
}

func (a *Account) IsOpenAIVideoRelayDedicated() bool {
	if a == nil {
		return false
	}
	_, present := a.Credentials[OpenAIVideoRelayCredentialsKey]
	return present
}

func (a *Account) OpenAIVideoRelaySettings() (OpenAIVideoRelaySettings, bool) {
	if a == nil {
		return OpenAIVideoRelaySettings{}, false
	}
	return parseOpenAIVideoRelaySettings(a.Credentials[OpenAIVideoRelayCredentialsKey])
}

func parseOpenAIVideoRelaySettings(raw any) (OpenAIVideoRelaySettings, bool) {
	var result OpenAIVideoRelaySettings
	if raw == nil {
		return result, false
	}
	payload, err := json.Marshal(raw)
	if err != nil || json.Unmarshal(payload, &result) != nil ||
		result.MinSeconds < 1 || result.MinSeconds > result.DefaultSeconds ||
		result.DefaultSeconds > result.MaxSeconds || result.MaxSeconds > math.MaxInt32 ||
		len(result.Resolutions) == 0 {
		return OpenAIVideoRelaySettings{}, false
	}
	resolutions := make([]string, 0, len(result.Resolutions))
	for _, rawResolution := range result.Resolutions {
		resolution, ok := LookupVideoBillingResolution(rawResolution)
		if !ok {
			return OpenAIVideoRelaySettings{}, false
		}
		if !slices.Contains(resolutions, resolution) {
			resolutions = append(resolutions, resolution)
		}
	}
	order := []string{VideoBillingResolution480P, VideoBillingResolution720P, VideoBillingResolution1080P}
	slices.SortFunc(resolutions, func(a, b string) int { return slices.Index(order, a) - slices.Index(order, b) })
	result.Resolutions = resolutions
	return result, true
}

func (config OpenAIVideoRelaySettings) resolve(request VideoRelayRequest) (seconds int, resolution string, allowed bool) {
	seconds, resolution = request.Seconds, request.Resolution
	if seconds == 0 {
		seconds = config.DefaultSeconds
	}
	if resolution == "" {
		resolution = config.Resolutions[0]
	}
	allowed = seconds >= config.MinSeconds && seconds <= config.MaxSeconds && slices.Contains(config.Resolutions, resolution)
	return
}

func NormalizeOpenAIVideoRelayCredentials(platform, accountType string, credentials map[string]any) error {
	account := &Account{Platform: platform, Type: accountType, Credentials: credentials}
	configured, _ := account.openAIEndpointCapabilitySet()
	enabled := configured[string(OpenAIEndpointCapabilityVideos)]
	if !enabled && !account.IsOpenAIVideoRelayDedicated() {
		return nil
	}
	config, valid := account.OpenAIVideoRelaySettings()
	if !valid {
		return infraerrors.BadRequest("INVALID_OPENAI_VIDEO_SETTINGS",
			"Choose at least one video resolution and keep the minimum, default, and maximum seconds in order.")
	}
	if enabled && (platform != PlatformOpenAI || accountType != AccountTypeAPIKey ||
		strings.TrimSpace(account.GetCredential("base_url")) == "") {
		return infraerrors.BadRequest("INVALID_OPENAI_VIDEO_ACCOUNT", "Enable video only for an OpenAI API Key account with a base URL.")
	}
	credentials[OpenAIVideoRelayCredentialsKey] = map[string]any{
		"resolutions": config.Resolutions, "min_seconds": config.MinSeconds,
		"max_seconds": config.MaxSeconds, "default_seconds": config.DefaultSeconds,
	}
	return nil
}
