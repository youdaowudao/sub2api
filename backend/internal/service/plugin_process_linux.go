//go:build linux

package service

import (
	"os/exec"
	"syscall"
)

// Linux sends Pdeathsig when the thread that created the child terminates.
// This is the kernel's creating-thread lifetime, which may end before the whole
// host process. Keep other process attributes intact; legacy never calls this.
func configureGenericPluginProcess(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Pdeathsig = syscall.SIGKILL
}
