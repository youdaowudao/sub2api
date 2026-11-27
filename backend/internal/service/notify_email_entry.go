package service

import (
	"encoding/json"
	"net/mail"
	"strings"
)

// NotifyEmailEntry represents a notification email with enable/disable and verification state.
// All emails are user-managed; maximum 3 entries per user.
type NotifyEmailEntry struct {
	Email    string `json:"email"`
	Disabled bool   `json:"disabled"`
	Verified bool   `json:"verified"`
}

// parseNotifyEmails parses a JSON string into []NotifyEmailEntry.
// It auto-detects the format:
//   - Old format ["email1","email2"] → converted to [{email, disabled:false, verified:true}, ...]
//   - New format [{email,disabled,verified}, ...] → parsed directly
//
// Returns nil on empty/invalid input.
func ParseNotifyEmails(raw string) []NotifyEmailEntry {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil
	}

	// Try parsing as new format first (array of objects)
	var entries []NotifyEmailEntry
	if err := json.Unmarshal([]byte(raw), &entries); err == nil && len(entries) > 0 {
		// Verify it's actually the new format by checking the first element
		// json.Unmarshal into []NotifyEmailEntry succeeds even for ["string"]
		// because it tries to fit "string" into NotifyEmailEntry and gets zero values.
		// We need to detect old format explicitly.
		if !isOldStringArrayFormat(raw) {
			return entries
		}
	}

	// Try parsing as old format (array of strings)
	var emails []string
	if err := json.Unmarshal([]byte(raw), &emails); err == nil {
		result := make([]NotifyEmailEntry, 0, len(emails))
		for _, e := range emails {
			e = strings.TrimSpace(e)
			if e != "" {
				result = append(result, NotifyEmailEntry{
					Email:    e,
					Disabled: false,
					Verified: false, // Old format emails default to unverified
				})
			}
		}
		return result
	}

	return nil
}

// isOldStringArrayFormat checks if the JSON is a string array like ["email1","email2"].
func isOldStringArrayFormat(raw string) bool {
	var arr []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &arr); err != nil || len(arr) == 0 {
		return false
	}
	// Check if first element starts with a quote (string) vs { (object)
	first := strings.TrimSpace(string(arr[0]))
	return len(first) > 0 && first[0] == '"'
}

// marshalNotifyEmails serializes []NotifyEmailEntry to JSON string.
func MarshalNotifyEmails(entries []NotifyEmailEntry) string {
	if len(entries) == 0 {
		return "[]"
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return "[]"
	}
	return string(data)
}

// filterVerifiedEmails returns deduplicated, non-disabled, verified emails.
func filterVerifiedEmails(entries []NotifyEmailEntry) []string {
	var recipients []string
	seen := make(map[string]bool)
	for _, entry := range entries {
		if entry.Disabled || !entry.Verified {
			continue
		}
		email := strings.TrimSpace(entry.Email)
		if email == "" {
			continue
		}
		lower := strings.ToLower(email)
		if seen[lower] {
			continue
		}
		seen[lower] = true
		recipients = append(recipients, email)
	}
	return recipients
}

// When no explicit list exists, the account email is the default recipient.
// A nonempty list keeps the user's explicit recipient choices, including disabled entries.
func balanceNotifyRecipients(accountEmail string, extraEmails []NotifyEmailEntry) []string {
	primary := strings.TrimSpace(accountEmail)
	if !usableBalanceNotifyEmail(primary) {
		primary = ""
	}
	if len(extraEmails) == 0 {
		if primary == "" {
			return nil
		}
		return []string{primary}
	}
	// Reuse the existing deduplication/disabled rules. The current account email
	// needs no extra verification; never persist this as a verified alternate address.
	entries := make([]NotifyEmailEntry, 0, len(extraEmails))
	for _, entry := range extraEmails {
		if !usableBalanceNotifyEmail(entry.Email) {
			continue
		}
		if primary != "" && strings.EqualFold(strings.TrimSpace(entry.Email), primary) {
			entry.Verified = true
		}
		entries = append(entries, entry)
	}
	return filterVerifiedEmails(entries)
}

func usableBalanceNotifyEmail(email string) bool {
	email = strings.TrimSpace(email)
	if email == "" || len(email) > 255 || strings.HasSuffix(strings.ToLower(email), ".invalid") {
		return false
	}
	address, err := mail.ParseAddress(email)
	return err == nil && address.Address == email
}
