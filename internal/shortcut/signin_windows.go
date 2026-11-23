package shortcut

import (
	"errors"
	"strings"

	"golang.org/x/sys/windows/registry"
)

var (
	runKey      = `Software\Microsoft\Windows\CurrentVersion\Run`
	approvedKey = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`
)

const runName = "Yoru"

func signInCommand(exe string) string { return `"` + exe + `" ` + TrayArg }

func AtSignIn(exe string) (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false, nil
	}
	defer k.Close()
	v, _, err := k.GetStringValue(runName)
	if err != nil || !strings.EqualFold(v, signInCommand(exe)) {
		return false, nil
	}
	if a, err := registry.OpenKey(registry.CURRENT_USER, approvedKey, registry.QUERY_VALUE); err == nil {
		defer a.Close()
		if b, _, err := a.GetBinaryValue(runName); err == nil && len(b) > 0 && b[0]&1 == 1 {
			return false, nil
		}
	}
	return true, nil
}

func SetAtSignIn(exe string, on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if on {
		err = k.SetStringValue(runName, signInCommand(exe))
	} else if err = k.DeleteValue(runName); errors.Is(err, registry.ErrNotExist) {
		err = nil
	}
	if err != nil {
		return err
	}
	if a, err := registry.OpenKey(registry.CURRENT_USER, approvedKey, registry.SET_VALUE); err == nil {
		defer a.Close()
		a.DeleteValue(runName)
	}
	return nil
}
