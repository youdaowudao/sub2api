//go:build unit

package service

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCheckinAuditRedaction(t *testing.T) {
	// Values are synthetic test canaries, never real credentials.
	raw := []byte(`{"captcha_token":"fixture-token-canary","captcha_randstr":"fixture-randstr-canary","email_code":"fixture-email-code-canary"}`)
	out := RedactAuditBody(raw, "application/json")
	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &decoded))
	for _, s := range []string{"fixture-token-canary", "fixture-randstr-canary", "fixture-email-code-canary"} {
		require.NotContains(t, out, s)
	}
}
