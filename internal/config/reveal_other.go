//go:build !windows

package config

import (
	"os/exec"
	"path/filepath"
	"runtime"
)

func Reveal(path string) error {
	if runtime.GOOS == "darwin" {
		return started(exec.Command("open", "-R", path))
	}
	return started(exec.Command("xdg-open", filepath.Dir(path)))
}

func OpenFolder(path string) error {
	if runtime.GOOS == "darwin" {
		return started(exec.Command("open", path))
	}
	return started(exec.Command("xdg-open", path))
}

func started(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
