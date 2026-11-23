package config

import (
	"os/exec"
	"syscall"
)

const breakawayFromJob = 0x01000000

func detach(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= breakawayFromJob
}

func Detach(cmd *exec.Cmd) { detach(cmd) }
