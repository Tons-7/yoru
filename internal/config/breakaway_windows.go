package config

import (
	"os/exec"
	"syscall"
)

// CREATE_BREAKAWAY_FROM_JOB. Yoru runs in a job object so helpers cannot outlive it; the user's own browser
// breaks away so it survives Yoru quitting.
const breakawayFromJob = 0x01000000

func detach(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= breakawayFromJob
}

// Detach lets a child outlive Yoru: the browser window, or the binary an update hands over to.
func Detach(cmd *exec.Cmd) { detach(cmd) }
