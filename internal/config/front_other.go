//go:build !windows

package config

// toFront has no portable way to raise another program's window; Show opens one instead.
func toFront(int) bool { return false }

// AllowForeground is for Windows, which keeps a background process from raising a window.
func AllowForeground() {}
