//go:build !windows

package config

import (
	"os/exec"
	"syscall"
)

func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

func Detach(cmd *exec.Cmd) { detach(cmd) }
