//go:build !windows

package proc

import "os/exec"

// Hide is for Windows, where a console tool started by a windowed app opens a window of its own.
func Hide(cmd *exec.Cmd) *exec.Cmd { return cmd }

// Background runs a command under nice, for work nobody waits on.
func Background(cmd *exec.Cmd) *exec.Cmd {
	if nice, err := exec.LookPath("nice"); err == nil {
		cmd.Args = append([]string{nice, "-n", "10"}, cmd.Args...)
		cmd.Path = nice
	}
	return cmd
}
