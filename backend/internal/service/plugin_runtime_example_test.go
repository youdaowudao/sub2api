package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// This optional test uses an actual independently built, signed v1 executable.
// It uses the existing installer and runtime, rather than a mock Transport.
func TestPluginLegacyExampleNativeForward(t *testing.T) {
	packagePath := os.Getenv("SUB2API_TEST_LEGACY_PACKAGE")
	publicPath := os.Getenv("SUB2API_TEST_PLUGIN_PUBLIC_KEY_FILE")
	if packagePath == "" || publicPath == "" {
		t.Skip("未显式提供隔离的签名 v1 示例包与公钥文件")
	}
	publicKey, err := os.ReadFile(publicPath)
	require.NoError(t, err)
	file, err := os.Open(packagePath)
	require.NoError(t, err)
	defer file.Close()
	root := t.TempDir()
	cfg := testPluginConfig(root, false)
	cfg.Plugins.TrustedPublishers = map[string]string{"local-example": strings.TrimSpace(string(publicKey))}
	installation, err := NewPluginPackageInstaller(cfg, PluginHostInfo{Version: "0.2.16", BuildType: "release"}).Install(context.Background(), file, nil)
	require.NoError(t, err)
	require.Equal(t, 1, installation.Manifest.SchemaVersion)
	require.Equal(t, PluginSignatureTrusted, installation.SignatureStatus)
	runtime, err := startPluginRuntime(context.Background(), installation, 10*time.Second, filepath.Join(root, "runtime"), nil)
	require.NoError(t, err)
	defer runtime.kill()
	require.NoError(t, runtime.validateAndApplyConfig(context.Background(), []byte(`{"extra_headers":{"X-Example-Legacy":"preserved"}}`)))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cancel" {
			<-r.Context().Done()
			return
		}
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, "legacy-payload", string(body))
		require.Equal(t, "preserved", r.Header.Get("X-Example-Legacy"))
		w.Header().Set("X-Native-Transport", "v1")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("real-legacy-response"))
	}))
	defer upstream.Close()
	account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1}
	request, err := http.NewRequest(http.MethodPost, upstream.URL, bytes.NewBufferString("legacy-payload"))
	require.NoError(t, err)
	require.True(t, runtime.beginRequest())
	response, err := runtime.roundTrip(context.Background(), request, "", account)
	if err != nil {
		runtime.finishRequest()
	}
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusCreated, response.StatusCode)
	require.Equal(t, "v1", response.Header.Get("X-Native-Transport"))
	require.Equal(t, "real-legacy-response", string(body))

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	request, err = http.NewRequestWithContext(ctx, http.MethodGet, upstream.URL+"/cancel", nil)
	require.NoError(t, err)
	require.True(t, runtime.beginRequest())
	started := time.Now()
	_, err = runtime.roundTrip(ctx, request, "", account)
	runtime.finishRequest()
	require.Error(t, err)
	require.True(t, errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled))
	require.Less(t, time.Since(started), 2*time.Second)
}
