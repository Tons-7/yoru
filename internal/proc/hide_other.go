//go:build !windows

package proc

import "os/exec"

func Hide(cmd *exec.Cmd) *exec.Cmd { return cmd }

func Background(cmd *exec.Cmd) *exec.Cmd {
	if nice, err := exec.LookPath("nice"); err == nil {
		cmd.Args = append([]string{nice, "-n", "10"}, cmd.Args...)
		cmd.Path = nice
	}
	return cmd
}
