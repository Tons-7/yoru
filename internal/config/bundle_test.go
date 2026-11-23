package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBundleRoot(t *testing.T) {
	home := filepath.FromSlash("/Users/me")
	if got, ok := bundleRoot(filepath.FromSlash("/Applications/Yoru.app/Contents/MacOS"), home); !ok ||
		got != filepath.Join(home, "Library", "Application Support", "Yoru") {
		t.Errorf("in a bundle: %q, %v", got, ok)
	}
	if _, ok := bundleRoot(filepath.FromSlash("/Users/me/Downloads/yoru"), home); ok {
		t.Error("a plain folder was read as an app bundle")
	}
}

func TestWithPackageDirs(t *testing.T) {
	sep := string(filepath.ListSeparator)
	got := withPackageDirs(strings.Join([]string{"/usr/bin", "/usr/local/bin"}, sep))
	want := strings.Join([]string{"/usr/bin", "/usr/local/bin", "/opt/homebrew/bin", "/opt/local/bin"}, sep)
	if got != want {
		t.Errorf("PATH = %q, want %q", got, want)
	}
}
