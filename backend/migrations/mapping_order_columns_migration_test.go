package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMappingOrderColumnsMigration(t *testing.T) {
	content, err := FS.ReadFile("258_mapping_order_columns.sql")
	require.NoError(t, err)
	sql := string(content)
	require.Contains(t, sql, "SET LOCAL lock_timeout = '5s';")
	require.Contains(t, sql, "ALTER TABLE channels ADD COLUMN IF NOT EXISTS model_mapping_order JSONB NULL;")
	require.Contains(t, sql, "ALTER TABLE groups ADD COLUMN IF NOT EXISTS model_routing_order JSONB NULL;")
	require.NotContains(t, sql, "DEFAULT")
	require.NotContains(t, sql, "UPDATE ")
	require.NotContains(t, sql, "CREATE INDEX")
}
