package migrations

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestBalanceNotifyOptInMigration(t *testing.T) {
	raw, err := FS.ReadFile("257_balance_notify_opt_in.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(raw)), " ")
	require.Contains(t, sql, "ALTER TABLE users ALTER COLUMN balance_notify_enabled SET DEFAULT false;")
	require.Contains(t, sql, "UPDATE users SET balance_notify_enabled = false WHERE balance_notify_enabled = true;")
	raw, err = FS.ReadFile("141_subscription_expiry_notify_enabled.sql")
	require.NoError(t, err)
	require.Contains(t, string(raw), "VALUES ('subscription_expiry_notify_enabled', 'false', NOW())")
	require.Contains(t, string(raw), "ON CONFLICT (key) DO NOTHING")
}
