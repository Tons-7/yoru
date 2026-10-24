package shortcut

import (
	"errors"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// Windows' per-user startup list, as Task Manager's Startup apps shows it; vars so tests use their own key.
var (
	runKey      = `Software\Microsoft\Windows\CurrentVersion\Run`
	approvedKey = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run`
)

const runName = "Yoru"

func signInCommand(exe string) string { return `"` + exe + `" ` + TrayArg }

// AtSignIn: Yoru starts in the tray when this user signs in, as exe.
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
	// Task Manager's Disable keeps the entry and marks it off here, with an odd first byte.
	if a, err := registry.OpenKey(registry.CURRENT_USER, approvedKey, registry.QUERY_VALUE); err == nil {
		defer a.Close()
		if b, _, err := a.GetBinaryValue(runName); err == nil && len(b) > 0 && b[0]&1 == 1 {
			return false, nil
		}
	}
	return true, nil
}

// SetAtSignIn adds Yoru to the startup list or takes it off; turned on, a Disable from Task Manager is undone.
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
