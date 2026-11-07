package service

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func inspectGenericManifestShape(t *testing.T, kind string, edit func(map[string]any)) error {
	t.Helper()
	manifest := extensionContractManifest(t, kind)
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	var shape map[string]any
	require.NoError(t, json.Unmarshal(raw, &shape))
	if edit != nil {
		edit(shape)
	}
	raw, err = json.Marshal(shape)
	require.NoError(t, err)
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	entry, err := writer.Create("manifest.json")
	require.NoError(t, err)
	_, err = entry.Write(raw)
	require.NoError(t, err)
	for name := range manifest.Files {
		entry, err = writer.Create(name)
		require.NoError(t, err)
		contents := "binary"
		if name == "ui/index.html" {
			contents = "<html></html>"
		}
		_, err = entry.Write([]byte(contents))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	archive, err := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	require.NoError(t, err)
	installer := NewPluginPackageInstaller(testPluginConfig(t.TempDir(), true), PluginHostInfo{Version: "0.2.16"})
	_, _, _, err = installer.inspectArchive(archive)
	return err
}

func TestPluginManifestShapeRequiresExplicitProtocols(t *testing.T) {
	fields := []string{"plugin_protocol", "transport_api", "backend_api", "host_service_api", "ui_bridge"}
	for _, kind := range []string{"frontend", "backend", "combined"} {
		t.Run(kind+"/explicit_zero_is_valid", func(t *testing.T) {
			require.NoError(t, inspectGenericManifestShape(t, kind, nil))
		})
		for _, field := range fields {
			for _, mode := range []string{"missing", "null"} {
				t.Run(kind+"/"+field+"/"+mode, func(t *testing.T) {
					err := inspectGenericManifestShape(t, kind, func(shape map[string]any) {
						requires := shape["requires"].(map[string]any)
						if mode == "missing" {
							delete(requires, field)
						} else {
							requires[field] = nil
						}
					})
					require.Error(t, err, "schema 2 requires explicit non-null negotiation fields, including zero versions")
				})
			}
		}
	}
}

func TestPluginManifestShapeRequiresCapabilityArrayAndRejectsNullFields(t *testing.T) {
	for _, field := range []string{"capabilities", "runtimes", "backend", "description", "author"} {
		t.Run(field+"/null", func(t *testing.T) {
			require.Error(t, inspectGenericManifestShape(t, "frontend", func(shape map[string]any) { shape[field] = nil }))
		})
	}
	t.Run("capabilities/missing", func(t *testing.T) {
		require.Error(t, inspectGenericManifestShape(t, "frontend", func(shape map[string]any) { delete(shape, "capabilities") }))
	})
}
