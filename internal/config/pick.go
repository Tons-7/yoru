package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrPickCancelled = errors.New("no folder was picked")
	ErrPickBusy      = errors.New("a folder window is already open")
	ErrNoPicker      = errors.New("this computer has no folder window Yoru can open")
)

func pickStart(start string) string {
	start = filepath.Clean(start)
	if !filepath.IsAbs(start) {
		return ""
	}
	if strings.Contains(start, "..") {
		return ""
	}
	if info, err := os.Stat(start); err != nil || !info.IsDir() {
		return ""
	}
	return start
}
