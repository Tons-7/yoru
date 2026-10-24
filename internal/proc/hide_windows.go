package proc

import (
	"os/exec"
	"syscall"
)

const (
	createNoWindow           = 0x08000000
	belowNormalPriorityClass = 0x00004000
)

// Background is Hide at below-normal priority, for work nobody waits on (an episode's sound fingerprinted).
func Background(cmd *exec.Cmd) *exec.Cmd {
	Hide(cmd).SysProcAttr.CreationFlags |= belowNormalPriorityClass
	return cmd
}

// Hide starts a console tool (ffmpeg, ffprobe, tar, PowerShell) without a window: Yoru is a Windows app with
// no console to lend it, so each run would flash one. Not for programs with a window to show (mpv, VLC).
func Hide(cmd *exec.Cmd) *exec.Cmd {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
	return cmd
}
