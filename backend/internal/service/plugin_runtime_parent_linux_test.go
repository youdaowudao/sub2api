//go:build linux

package service

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// An actual host-parent SIGKILL must not leave a trusted Backend executable alive.
func TestPluginGenericNativeParentExit(t *testing.T) {
	packagePath := os.Getenv("SUB2API_TEST_GENERIC_PACKAGE")
	publicPath := os.Getenv("SUB2API_TEST_PLUGIN_PUBLIC_KEY_FILE")
	if packagePath == "" || publicPath == "" {
		t.Skip("未显式提供隔离的签名 Backend 示例包与公钥")
	}
	if root := os.Getenv("SUB2API_TEST_PLUGIN_PARENT_HELPER_ROOT"); root != "" {
		publicKey, err := os.ReadFile(publicPath)
		require.NoError(t, err)
		cfg := testPluginConfig(root, false)
		cfg.Plugins.TrustedPublishers = map[string]string{"local-example": strings.TrimSpace(string(publicKey))}
		file, err := os.Open(packagePath)
		require.NoError(t, err)
		defer func() { require.NoError(t, file.Close()) }()
		installation, err := NewPluginPackageInstaller(cfg, PluginHostInfo{Version: "0.2.16"}).Install(context.Background(), file, nil)
		require.NoError(t, err)
		host := newPluginHostServiceServer(installation.PluginKey, nil, nil, PluginAccountScope{})
		runtime, err := startPluginRuntime(context.Background(), installation, 10*time.Second, filepath.Join(root, "rpc"), host)
		require.NoError(t, err)
		fmt.Printf("PLUGIN_PROCESS_PID=%d\n", runtime.client.ReattachConfig().Pid)
		select {}
	}
	root, err := os.MkdirTemp("", "pp-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	executable, err := os.Executable()
	require.NoError(t, err)
	parent := exec.Command(executable, "-test.run=^TestPluginGenericNativeParentExit$", "-test.timeout=20s")
	parent.Env = append(os.Environ(), "SUB2API_TEST_PLUGIN_PARENT_HELPER_ROOT="+root)
	stdout, err := parent.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, parent.Start())
	t.Cleanup(func() { _ = parent.Process.Kill(); _ = parent.Wait() })
	ready := make(chan int, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if value, ok := strings.CutPrefix(scanner.Text(), "PLUGIN_PROCESS_PID="); ok {
				pid, _ := strconv.Atoi(value)
				ready <- pid
				return
			}
		}
		ready <- 0
	}()
	var pid int
	select {
	case pid = <-ready:
	case <-time.After(15 * time.Second):
		t.Fatal("真实 Backend 启动超时")
	}
	require.Positive(t, pid)
	processPath := filepath.Join("/proc", strconv.Itoa(pid))
	owned := func() bool {
		exe, err := os.Readlink(filepath.Join(processPath, "exe"))
		return err == nil && strings.HasPrefix(exe, root+string(os.PathSeparator))
	}
	require.True(t, owned(), "只能操作本测试创建的 Backend 进程")
	t.Cleanup(func() {
		if owned() {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	require.NoError(t, parent.Process.Kill())
	_ = parent.Wait()
	require.Eventually(t, func() bool {
		stat, err := os.ReadFile(filepath.Join(processPath, "stat"))
		return os.IsNotExist(err) || (err == nil && strings.Contains(string(stat), ") Z "))
	}, 2*time.Second, 20*time.Millisecond, "宿主异常退出后不能留下 Backend 进程")
}
