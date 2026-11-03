//go:build !windows

package config

import (
	"os"
	"syscall"
)

func toFront(int) bool { return false }

func askClose(pid int) bool {
	p, err := os.FindProcess(pid)
	return err == nil && p.Signal(syscall.SIGTERM) == nil
}

func AllowForeground() {}
