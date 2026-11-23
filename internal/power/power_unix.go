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

func hold(why string) (func(), error) {
	pid := strconv.Itoa(os.Getpid())
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("caffeinate", "-i", "-w", pid)
	default:
		cmd = exec.Command("systemd-inhibit", "--what=idle:sleep", "--who=Yoru", "--why="+why, "--mode=block",
			"sh", "-c", "while kill -0 "+pid+" 2>/dev/null; do sleep 5; done")
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%s: %w", cmd.Path, err)
	}
	go cmd.Wait()
	return func() { syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }, nil
}
