//go:build !windows

package config

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

var pickMu sync.Mutex

func linuxPicker() string {
	for _, name := range []string{"zenity", "kdialog"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

func CanPick() bool { return runtime.GOOS == "darwin" || linuxPicker() != "" }

func appleText(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func PickFolder(title, start string) (string, error) {
	if !pickMu.TryLock() {
		return "", ErrPickBusy
	}
	defer pickMu.Unlock()
	if info, err := os.Stat(start); err != nil || !info.IsDir() {
		start = ""
	}

	var cmd *exec.Cmd
	switch picker := linuxPicker(); {
	case runtime.GOOS == "darwin":
		ask := "choose folder with prompt " + appleText(title)
		if start != "" {
			ask += " default location POSIX file " + appleText(start)
		}
		cmd = exec.Command("osascript",
			"-e", `tell application "System Events"`,
			"-e", "activate",
			"-e", "set picked to "+ask,
			"-e", "end tell",
			"-e", "return POSIX path of picked")
	case strings.HasSuffix(picker, "zenity"):
		args := []string{"--file-selection", "--directory", "--title=" + title}
		if start != "" {
			args = append(args, "--filename="+strings.TrimRight(start, "/")+"/")
		}
		cmd = exec.Command(picker, args...)
	case picker != "":
		cmd = exec.Command(picker, "--getexistingdirectory", start, "--title", title)
	default:
		return "", ErrNoPicker
	}

	out, err := cmd.Output()
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		said := strings.TrimSpace(string(exit.Stderr))
		if runtime.GOOS == "darwin" && strings.Contains(said, "-128") || runtime.GOOS != "darwin" && exit.ExitCode() == 1 {
			return "", ErrPickCancelled
		}
		if said == "" {
			said = exit.Error()
		}
		return "", errors.New("the folder window failed: " + said)
	}
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(out))
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
	}
	if path == "" {
		return "", ErrPickCancelled
	}
	return path, nil
}
