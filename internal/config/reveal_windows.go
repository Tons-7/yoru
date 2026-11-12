package config

import (
	"os/exec"
	"path/filepath"
	"syscall"
)

func Reveal(path string) error {
	return explore(`explorer /select,"`+path+`"`, filepath.Dir(path))
}

func OpenFolder(path string) error {
	return explore(`explorer "`+path+`"`, path)
}

func explore(line, shows string) error {
	before := shellWindows()
	cmd := exec.Command("explorer")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: line}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	go raiseShellWindow(filepath.Clean(shows), before)
	return nil
}
