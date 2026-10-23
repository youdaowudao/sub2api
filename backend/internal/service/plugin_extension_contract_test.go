package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// These expectations cover the three real package forms without depending on
// new Go fields: the original implementation rejects them as schema 2.
func extensionContractManifest(t *testing.T, kind string) PluginManifest {
	t.Helper()
	requires := map[string]any{"sub2api": ">=0.0.0", "plugin_protocol": 0, "transport_api": 0, "backend_api": 0, "host_service_api": 0, "ui_bridge": 0}
	files := map[string]string{}
	manifest := map[string]any{"schema_version": 2, "id": "example." + kind, "name": kind, "version": "1.0.0", "requires": requires, "capabilities": []any{}, "runtimes": map[string]any{}, "files": files}
	if kind != "backend" {
		requires["ui_bridge"] = 2
		manifest["ui"] = map[string]any{"entries": []any{map[string]any{"id": "home", "label": "Example", "path": "ui/index.html", "mount": "user", "roles": []string{"user", "admin"}}}}
		hash := sha256.Sum256([]byte("<html></html>"))
		files["ui/index.html"] = hex.EncodeToString(hash[:])
	}
	if kind != "frontend" {
		requires["plugin_protocol"], requires["backend_api"], requires["host_service_api"] = 1, 1, 3
		manifest["runtimes"] = map[string]any{PluginManifest{}.RuntimeKey(): map[string]any{"path": "bin/plugin"}}
		manifest["capabilities"] = []any{map[string]any{"id": "backend.invoke.v1"}}
		manifest["backend"] = map[string]any{"methods": []any{map[string]any{"name": "echo", "role": "user", "access": "read"}}}
		hash := sha256.Sum256([]byte("binary"))
		files["bin/plugin"] = hex.EncodeToString(hash[:])
	}
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	var result PluginManifest
	require.NoError(t, json.Unmarshal(raw, &result))
	return result
}

func TestPluginExtensionContractThreePackageForms(t *testing.T) {
	for _, kind := range []string{"frontend", "backend", "combined"} {
		t.Run(kind, func(t *testing.T) {
			manifest := extensionContractManifest(t, kind)
			require.NoError(t, manifest.Validate(), "a generic plugin must not implement an unused Transport or UI")
			require.True(t, EvaluatePluginCompatibility(manifest, PluginHostInfo{Version: "0.1.179"}).Compatible)
		})
	}
}

func TestPluginExtensionContractFrontendInstallsWithoutExecutable(t *testing.T) {
	manifest := extensionContractManifest(t, "frontend")
	installer := NewPluginPackageInstaller(testPluginConfig(t.TempDir(), true), PluginHostInfo{Version: "0.1.179"})
	archive := buildPluginArchive(t, manifest, nil, "", nil)
	installed, err := installer.Install(context.Background(), bytes.NewReader(archive), nil)
	require.NoError(t, err)
	require.Empty(t, installed.BinaryPath, "UI-only packages must not carry or start a dummy binary")
	require.Equal(t, PluginStateDisabled, installed.State)
	require.Len(t, installed.BinarySHA256, 64, "UI-only revisions must still support replacement CAS")
}

func TestPluginExtensionContractKeepsLegacyCapabilityValidation(t *testing.T) {
	manifest := testPluginManifest(nil)
	require.NoError(t, manifest.Validate())
	manifest.Capabilities[0].ID = "backend.invoke.v1"
	require.Error(t, manifest.Validate(), "schema 1 must not gain generic privileges")
	manifest = testPluginManifest(nil)
	manifest.Files["../escape"] = strings.Repeat("0", 64)
	require.Error(t, manifest.Validate())
}

func TestPluginExtensionContractGenericHostRequiresActiveBusinessGrant(t *testing.T) {
	store := newFakePluginKVStore()
	manager := &PluginManager{kvStore: store}
	manifest := extensionContractManifest(t, "backend")
	manifest.Capabilities = append(manifest.Capabilities, PluginCapability{ID: "storage.read.v1"}, PluginCapability{ID: "storage.write.v1"})
	host := manager.buildHostServices(&PluginInstallation{ID: 71, PluginKey: manifest.ID, Manifest: manifest, State: PluginStateEnabled})
	require.NotNil(t, host)
	_, err := host.KVSet(context.Background(), &pluginv1.KVSetRequest{Namespace: "state", Key: "unauthorized", Value: []byte("must not be written")})
	require.Error(t, err, "a generic runtime outside Invoke/RunOnce must not gain the legacy token-free KV privilege")
	require.Contains(t, []codes.Code{codes.PermissionDenied, codes.Unauthenticated}, status.Code(err))
	_, found, err := store.Get(context.Background(), manifest.ID, "state", "unauthorized")
	require.NoError(t, err)
	require.False(t, found)
}

func TestPluginExtensionContractLegacyHostKeepsTokenFreeKV(t *testing.T) {
	store := newFakePluginKVStore()
	manager := &PluginManager{kvStore: store}
	manifest := testPluginManifest(nil)
	host := manager.buildHostServices(&PluginInstallation{ID: 72, PluginKey: manifest.ID, Manifest: manifest})
	require.NotNil(t, host)
	_, err := host.KVSet(context.Background(), &pluginv1.KVSetRequest{Namespace: "state", Key: "legacy", Value: []byte("retained")})
	require.NoError(t, err)
}
