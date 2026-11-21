package service

import (
	"slices"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var groupDisplayIconPlatforms = []string{
	"anthropic", "openai", "gemini", "antigravity", "grok", "kimi", "zhipu",
	"minimax", "deepseek", "opencode_go", "command_code", "cline",
}

var groupDisplayCategories = []string{"openai", "anthropic", "gemini", "domestic"}

func normalizeGroupDisplayIcon(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value != "" && !slices.Contains(groupDisplayIconPlatforms, value) {
		return "", infraerrors.BadRequest("INVALID_GROUP_DISPLAY_ICON", "display_icon must be empty or a platform id with a dedicated logo")
	}
	return value, nil
}

func normalizeGroupDisplayCategory(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value != "" && !slices.Contains(groupDisplayCategories, value) {
		return "", infraerrors.BadRequest("INVALID_GROUP_DISPLAY_CATEGORY", "display_category must be empty or one of openai, anthropic, gemini, domestic")
	}
	return value, nil
}
