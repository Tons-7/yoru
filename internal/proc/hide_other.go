//go:build !windows

package proc

import "os/exec"

// Hide is for Windows, where a console tool started by a windowed app opens a window of its own.
func Hide(cmd *exec.Cmd) *exec.Cmd { return cmd }
