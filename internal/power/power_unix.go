//go:build !windows

package power

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"syscall"
)

// hold runs the system's own tool for as long as the request lasts; both end by themselves if Yoru dies.
func hold(why string) (func(), error) {
	pid := strconv.Itoa(os.Getpid())
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("caffeinate", "-i", "-w", pid)
	default:
		cmd = exec.Command("systemd-inhibit", "--what=idle:sleep", "--who=Yoru", "--why="+why, "--mode=block",
			"tail", "--pid="+pid, "-f", "/dev/null")
	}
	// A group of its own, ended whole: killing systemd-inhibit alone leaves its tail running until Yoru exits.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%s: %w", cmd.Path, err)
	}
	go cmd.Wait()
	return func() { syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }, nil
}
