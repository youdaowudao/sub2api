//go:build !linux

package service

import "os/exec"

func configureGenericPluginProcess(_ *exec.Cmd) {}
