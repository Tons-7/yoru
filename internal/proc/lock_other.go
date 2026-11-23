//go:build !windows

package proc

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"
)

func Lock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

func Alert(title, text string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if runtime.GOOS == "darwin" {
		exec.CommandContext(ctx, "osascript",
			"-e", "on run argv",
			"-e", "display alert (item 1 of argv) message (item 2 of argv) as critical",
			"-e", "end run", title, text).Run()
		return
	}
	for _, try := range [][]string{
		{"zenity", "--error", "--no-markup", "--title", title, "--text", text},
		{"kdialog", "--title", title, "--error", text},
		{"notify-send", "--urgency=critical", title, text},
	} {
		if _, err := exec.LookPath(try[0]); err != nil {
			continue
		}
		exec.CommandContext(ctx, try[0], try[1:]...).Run()
		return
	}
}
