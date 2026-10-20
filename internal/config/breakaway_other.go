//go:build !windows

package config

import (
	"os/exec"
	"syscall"
)

// A session of its own: the terminal or launchd job ending with the old process doesn't take the new one along.
// A systemd unit's cgroup still does; Restart=always brings Yoru back there.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

func Detach(cmd *exec.Cmd) { detach(cmd) }
