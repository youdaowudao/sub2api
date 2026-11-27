package schema

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestUserBalanceNotifyDefaultDisabled(t *testing.T) {
	for _, field := range (User{}).Fields() {
		d := field.Descriptor()
		if d.Name == "balance_notify_enabled" {
			require.Equal(t, false, d.Default)
			return
		}
	}
	t.Fatal("balance notification default is missing")
}
