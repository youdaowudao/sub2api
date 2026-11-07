package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type genericAssetUsers struct {
	UserRepository
	mu   sync.Mutex
	user User
}

func (r *genericAssetUsers) GetByID(_ context.Context, id int64) (*User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.user.ID != id {
		return nil, ErrUserNotFound
	}
	user := r.user
	return &user, nil
}
func (r *genericAssetUsers) change(fn func(*User)) { r.mu.Lock(); defer r.mu.Unlock(); fn(&r.user) }

func TestPluginBusinessAssetAuthorityAndPurpose(t *testing.T) {
	ctx := context.Background()
	cfg := testPluginConfig(t.TempDir(), true)
	repo := &genericSecurityRepo{}
	manager := NewPluginManager(repo, pluginTokenEncryptor{}, cfg, PluginHostInfo{Version: "0.1.179"}, nil)
	manifest := extensionContractManifest(t, "frontend")
	manifest.UI.Entrypoint = "ui/index.html"
	_, err := manager.Install(ctx, bytes.NewReader(buildPluginArchive(t, manifest, nil, "", nil)), nil)
	require.NoError(t, err)
	_, err = manager.Enable(ctx, 171, true, 100)
	require.NoError(t, err)
	users := &genericAssetUsers{user: User{ID: 7, Role: RoleUser, Status: StatusActive, Email: "private@example.test", PasswordHash: "private-password"}}
	manager.SetExtensionServices(nil, users, nil)
	caller := PluginCaller{UserID: 7, Role: RoleUser}
	session, err := manager.CreateEntrySession(ctx, 171, "home", caller)
	require.NoError(t, err)
	require.Equal(t, 2, session.UIBridgeVersion)
	require.NotEmpty(t, session.BridgeToken)
	sessionURL, err := url.Parse(session.URL)
	require.NoError(t, err)
	require.Equal(t, "bridge_token="+url.QueryEscape(session.BridgeToken), sessionURL.Fragment)
	prefix := "/api/v1/plugin-entry-ui/171/"
	require.True(t, strings.HasPrefix(sessionURL.Path, prefix))
	token, relative, ok := strings.Cut(strings.TrimPrefix(sessionURL.Path, prefix), "/")
	require.True(t, ok)
	data, logical, err := manager.ResolveBusinessUIAsset(ctx, 171, relative, token)
	require.NoError(t, err)
	require.Equal(t, "ui/index.html", logical)
	require.Equal(t, "<html></html>", string(data))
	raw, err := json.Marshal(session)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "private@example.test")
	require.NotContains(t, string(raw), "private-password")
	_, err = manager.ResolveUIAssetToken(token)
	require.Error(t, err)
	configToken, _, err := manager.CreateUIAssetToken(ctx, 171, 30*time.Minute)
	require.NoError(t, err)
	_, _, err = manager.ResolveBusinessUIAsset(ctx, 171, relative, configToken)
	require.Error(t, err)
	_, _, err = manager.ResolveBusinessUIAsset(ctx, 172, relative, token)
	require.Error(t, err)
	_, _, err = manager.ResolveBusinessUIAsset(ctx, 171, "../manifest.json", token)
	require.Error(t, err)
	_, err = manager.CreateEntrySession(ctx, 171, "unknown", caller)
	require.Error(t, err)

	repo.change(func(value *PluginInstallation) { value.State = PluginStateDisabled })
	_, _, err = manager.ResolveBusinessUIAsset(ctx, 171, relative, token)
	require.Error(t, err)
	resolved, err := manager.ResolveUIAssetToken(configToken)
	require.NoError(t, err)
	require.Equal(t, int64(171), resolved)
	_, _, err = manager.ReadUIAsset(ctx, 171, "index.html")
	require.NoError(t, err, "legacy management resources remain available while disabled")
	repo.change(func(value *PluginInstallation) { value.State = PluginStateEnabled })
	users.change(func(user *User) { user.Role = RoleAdmin })
	_, _, err = manager.ResolveBusinessUIAsset(ctx, 171, relative, token)
	require.Error(t, err)
	users.change(func(user *User) { user.Role = RoleUser; user.PasswordHash = "changed-password" })
	_, _, err = manager.ResolveBusinessUIAsset(ctx, 171, relative, token)
	require.Error(t, err)
	users.change(func(user *User) { user.PasswordHash = "private-password"; user.Status = "disabled" })
	_, _, err = manager.ResolveBusinessUIAsset(ctx, 171, relative, token)
	require.Error(t, err)
	users.change(func(user *User) { user.Status = StatusActive })
	repo.change(func(value *PluginInstallation) { value.BinarySHA256 = strings.Repeat("b", 64) })
	_, _, err = manager.ResolveBusinessUIAsset(ctx, 171, relative, token)
	require.Error(t, err)
	repo.change(func(value *PluginInstallation) { value.BinarySHA256 = session.Entry.Revision })
	claims, err := manager.businessUIClaims(token)
	require.NoError(t, err)
	claims.Expires = time.Now().Add(-time.Second).Unix()
	expiredRaw, err := json.Marshal(claims)
	require.NoError(t, err)
	encrypted, err := manager.encryptor.Encrypt(pluginBusinessTokenPrefix + string(expiredRaw))
	require.NoError(t, err)
	_, _, err = manager.ResolveBusinessUIAsset(ctx, 171, relative, base64.RawURLEncoding.EncodeToString([]byte(encrypted)))
	require.Error(t, err)
}
