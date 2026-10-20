package config

import (
	"os/exec"
	"syscall"
)

// Reveal opens Explorer on path's folder with path selected. The command line is written out whole: Explorer
// reads /select,"path" as one argument, which exec's quoting would split.
func Reveal(path string) error {
	cmd := exec.Command("explorer")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer /select,"` + path + `"`}
	return cmd.Start()
}
