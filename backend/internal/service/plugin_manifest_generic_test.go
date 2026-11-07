package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPluginGenericManifestRejectsPrivilegeAndVersionDrift(t *testing.T) {
	cases := []struct {
		name   string
		kind   string
		change func(*PluginManifest)
	}{
		{"backend version", "backend", func(m *PluginManifest) { m.Requires.BackendAPI = 2 }},
		{"host downgrade", "backend", func(m *PluginManifest) { m.Requires.HostServiceAPI = 2 }},
		{"transport capability", "backend", func(m *PluginManifest) {
			m.Capabilities = append(m.Capabilities, PluginCapability{ID: PluginCapabilityOpenAIOAuthOutbound})
		}},
		{"transport version", "backend", func(m *PluginManifest) { m.Requires.TransportAPI = 1 }},
		{"bridge downgrade", "combined", func(m *PluginManifest) { m.Requires.UIBridge = 1 }},
		{"pure UI capability", "frontend", func(m *PluginManifest) { m.Capabilities = []PluginCapability{{ID: PluginCapabilityStorageRead}} }},
		{"duplicate capability", "backend", func(m *PluginManifest) { m.Capabilities = append(m.Capabilities, m.Capabilities[0]) }},
		{"account scope", "backend", func(m *PluginManifest) { m.Capabilities[0].Platform = PlatformOpenAI }},
		{"write without read", "backend", func(m *PluginManifest) {
			m.Capabilities = append(m.Capabilities, PluginCapability{ID: PluginCapabilityStorageWrite})
		}},
		{"email without creations", "backend", func(m *PluginManifest) {
			m.Capabilities = append(m.Capabilities, PluginCapability{ID: PluginCapabilityUsersEmailRead})
		}},
		{"unknown method role", "backend", func(m *PluginManifest) { m.Backend.Methods[0].Role = "guest" }},
		{"unknown access", "backend", func(m *PluginManifest) { m.Backend.Methods[0].Access = "execute" }},
		{"invalid method", "backend", func(m *PluginManifest) { m.Backend.Methods[0].Name = "../echo" }},
		{"duplicate method", "backend", func(m *PluginManifest) { m.Backend.Methods = append(m.Backend.Methods, m.Backend.Methods[0]) }},
		{"method without invoke", "backend", func(m *PluginManifest) { m.Capabilities = []PluginCapability{{ID: PluginCapabilityBackendBackground}} }},
		{"too many methods", "backend", func(m *PluginManifest) {
			for i := 0; i < 32; i++ {
				m.Backend.Methods = append(m.Backend.Methods, PluginBackendMethod{Name: fmt.Sprintf("method%d", i), Role: RoleUser, Access: "read"})
			}
		}},
		{"missing current runtime", "backend", func(m *PluginManifest) {
			m.Runtimes = map[string]PluginRuntime{"unavailable-platform": {Path: "bin/plugin"}}
		}},
		{"undeclared executable", "backend", func(m *PluginManifest) { delete(m.Files, "bin/plugin") }},
		{"unsafe declared file", "frontend", func(m *PluginManifest) { m.Files["ui/../escape"] = strings.Repeat("a", 64) }},
		{"bad hash", "frontend", func(m *PluginManifest) { m.Files["ui/index.html"] = strings.Repeat("g", 64) }},
		{"undeclared UI", "frontend", func(m *PluginManifest) { m.UI.Entries[0].Path = "ui/missing.html" }},
		{"outside UI", "frontend", func(m *PluginManifest) {
			m.Files["index.html"] = strings.Repeat("a", 64)
			m.UI.Entries[0].Path = "index.html"
		}},
		{"invalid mount", "frontend", func(m *PluginManifest) { m.UI.Entries[0].Mount = "public" }},
		{"admin mount user role", "frontend", func(m *PluginManifest) { m.UI.Entries[0].Mount = "admin" }},
		{"empty roles", "frontend", func(m *PluginManifest) { m.UI.Entries[0].Roles = nil }},
		{"duplicate role", "frontend", func(m *PluginManifest) { m.UI.Entries[0].Roles = []string{RoleUser, RoleUser} }},
		{"duplicate entry", "frontend", func(m *PluginManifest) { m.UI.Entries = append(m.UI.Entries, m.UI.Entries[0]) }},
		{"empty plugin", "frontend", func(m *PluginManifest) { m.UI.Entries = nil; m.Requires.UIBridge = 0 }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			manifest := extensionContractManifest(t, test.kind)
			test.change(&manifest)
			require.Error(t, manifest.Validate())
		})
	}
}

func genericManifestOnlyArchive(t *testing.T, manifest map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	writeZipEntry(t, writer, pluginManifestFilename, raw)
	require.NoError(t, writer.Close())
	return buffer.Bytes()
}
func TestPluginGenericManifestRejectsUnknownFieldsBeforeExecution(t *testing.T) {
	for _, target := range []string{"root", "backend", "method", "entry", "legacy new field"} {
		t.Run(target, func(t *testing.T) {
			manifest := extensionContractManifest(t, "combined")
			if target == "legacy new field" {
				manifest = testPluginManifest(nil)
			}
			raw, err := json.Marshal(manifest)
			require.NoError(t, err)
			var shape map[string]any
			require.NoError(t, json.Unmarshal(raw, &shape))
			switch target {
			case "root":
				shape["unknown"] = true
			case "backend":
				backend, ok := shape["backend"].(map[string]any)
				require.True(t, ok)
				backend["unknown"] = true
			case "method":
				backend, ok := shape["backend"].(map[string]any)
				require.True(t, ok)
				methods, ok := backend["methods"].([]any)
				require.True(t, ok)
				require.NotEmpty(t, methods)
				method, ok := methods[0].(map[string]any)
				require.True(t, ok)
				method["unknown"] = true
			case "entry":
				ui, ok := shape["ui"].(map[string]any)
				require.True(t, ok)
				entries, ok := ui["entries"].([]any)
				require.True(t, ok)
				require.NotEmpty(t, entries)
				entry, ok := entries[0].(map[string]any)
				require.True(t, ok)
				entry["unknown"] = true
			case "legacy new field":
				requires, ok := shape["requires"].(map[string]any)
				require.True(t, ok)
				requires["backend_api"] = 0
			}
			installer := NewPluginPackageInstaller(testPluginConfig(t.TempDir(), true), PluginHostInfo{Version: "0.1.179"})
			_, err = installer.Install(context.Background(), bytes.NewReader(genericManifestOnlyArchive(t, shape)), nil)
			require.Error(t, err)
			if target == "legacy new field" {
				require.Contains(t, err.Error(), "旧版")
			} else {
				require.Contains(t, err.Error(), "unknown field")
			}
		})
	}
}
func TestPluginGenericManifestSerializationPreservesLegacyShape(t *testing.T) {
	for _, kind := range []string{"frontend", "backend", "combined", "legacy"} {
		t.Run(kind, func(t *testing.T) {
			manifest := testPluginManifest(nil)
			if kind != "legacy" {
				manifest = extensionContractManifest(t, kind)
			}
			raw, err := json.Marshal(manifest)
			require.NoError(t, err)
			var shape map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(raw, &shape))
			var req map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(shape["requires"], &req))
			if kind == "legacy" {
				require.NotContains(t, req, "backend_api")
				require.NotContains(t, req, "host_service_api")
				require.NotContains(t, shape, "backend")
			} else {
				require.Contains(t, req, "backend_api")
				require.Contains(t, req, "host_service_api")
			}
			var decoded PluginManifest
			require.NoError(t, json.Unmarshal(raw, &decoded))
			require.NoError(t, decoded.Validate())
		})
	}
}

func TestPluginGenericManifestKeepsDeclaredContractWithoutExtraLimits(t *testing.T) {
	t.Run("invoke without methods", func(t *testing.T) {
		manifest := extensionContractManifest(t, "backend")
		manifest.Backend.Methods = nil
		require.NoError(t, manifest.Validate())
	})
	t.Run("more than 32 entries", func(t *testing.T) {
		manifest := extensionContractManifest(t, "frontend")
		entry := manifest.UI.Entries[0]
		manifest.UI.Entries = nil
		for i := 0; i < 65; i++ {
			value := entry
			value.ID = fmt.Sprintf("entry%d", i)
			manifest.UI.Entries = append(manifest.UI.Entries, value)
		}
		require.NoError(t, manifest.Validate())
	})
}
