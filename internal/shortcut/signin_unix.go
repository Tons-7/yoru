//go:build !windows

package shortcut

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func signInFile() (string, error) {
	if runtime.GOOS == "darwin" {
		home, err := os.UserHomeDir()
		return filepath.Join(home, "Library", "LaunchAgents", "app.yoru.plist"), err
	}
	dir, err := os.UserConfigDir()
	return filepath.Join(dir, "autostart", "yoru.desktop"), err
}

func signInEntry(exe string) string {
	if runtime.GOOS == "darwin" {
		esc := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(exe)
		return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>app.yoru</string>
	<key>ProgramArguments</key><array><string>%s</string><string>%s</string></array>
	<key>RunAtLoad</key><true/>
</dict>
</plist>
`, esc, TrayArg)
	}
	quoted := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", `$`, `\$`).Replace(exe)
	field := strings.NewReplacer(`\`, `\\`, `%`, `%%`).Replace(quoted)
	return fmt.Sprintf("[Desktop Entry]\nType=Application\nName=Yoru\nExec=\"%s\" %s\nX-GNOME-Autostart-enabled=true\n", field, TrayArg)
}

func AtSignIn(exe string) (bool, error) {
	path, err := signInFile()
	if err != nil {
		return false, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil && string(b) == signInEntry(exe), err
}

func SetAtSignIn(exe string, on bool) error {
	path, err := signInFile()
	if err != nil {
		return err
	}
	if !on {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(signInEntry(exe)), 0o644)
}
