package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestForkNotesVisibility(t *testing.T) {
	const note = "[注册风险:高] 含+别名"
	user := &service.User{ID: 42, Email: "alias@example.com", Notes: note}
	// User-facing API key owners also use the public, shallow user DTO.
	user.APIKeys = []service.APIKey{{ID: 7, UserID: user.ID, User: user}}

	public := UserFromService(user)
	require.NotNil(t, public)
	require.Equal(t, user.ID, public.ID)
	publicJSON, err := json.Marshal(public)
	require.NoError(t, err)
	var publicFields map[string]any
	require.NoError(t, json.Unmarshal(publicJSON, &publicFields))
	require.NotContains(t, publicFields, "notes")
	require.NotContains(t, string(publicJSON), `"notes"`)
	require.NotContains(t, string(publicJSON), note)
	require.Len(t, public.APIKeys, 1)
	require.NotNil(t, public.APIKeys[0].User)
	require.Equal(t, user.ID, public.APIKeys[0].User.ID)

	admin := UserFromServiceAdmin(user)
	require.NotNil(t, admin)
	adminJSON, err := json.Marshal(admin)
	require.NoError(t, err)
	var adminFields map[string]any
	require.NoError(t, json.Unmarshal(adminJSON, &adminFields))
	require.Equal(t, note, adminFields["notes"])
	require.Contains(t, string(adminJSON), note)
}
