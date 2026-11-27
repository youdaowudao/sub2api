//go:build unit

package service

import "testing"

func TestGroupDisplayCategoryAllowlist(t *testing.T) {
	for _, value := range []string{"", "openai", "anthropic", "gemini", "domestic", "image"} {
		if got, err := normalizeGroupDisplayCategory(" " + value + " "); err != nil || got != value {
			t.Errorf("normalize %q: got %q, err %v", value, got, err)
		}
	}
	for _, value := range []string{"other", "grok", "invalid"} {
		if _, err := normalizeGroupDisplayCategory(value); err == nil {
			t.Errorf("unexpectedly accepted %q", value)
		}
	}
}
