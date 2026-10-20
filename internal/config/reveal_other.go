//go:build !windows

package config

import (
	"os/exec"
	"path/filepath"
	"runtime"
)

// Reveal shows path in the file manager: selected in the Finder, its folder elsewhere.
func Reveal(path string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", "-R", path).Start()
	}
	return exec.Command("xdg-open", filepath.Dir(path)).Start()
}
