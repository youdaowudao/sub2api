package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupDisplayIconCategoryMigration(t *testing.T) {
	content, err := FS.ReadFile("254_group_display_icon_category.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ALTER TABLE groups ADD COLUMN IF NOT EXISTS display_icon VARCHAR(32) NOT NULL DEFAULT '';")
	require.Contains(t, sql, "ALTER TABLE groups ADD COLUMN IF NOT EXISTS display_category VARCHAR(32) NOT NULL DEFAULT '';")
}
