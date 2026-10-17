//go:build !windows

// Package shortcut keeps the user's shortcuts opening Yoru after its program is renamed (kuro.exe to yoru.exe).
package shortcut

import "context"

// Retarget does nothing here: only Windows shortcuts name the program's file.
func Retarget(context.Context, string, string) (int, error) { return 0, nil }
