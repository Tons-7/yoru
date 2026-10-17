//go:build !windows

package discord

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
)

// openPipe dials the Discord app's socket n, wherever this system's Discord (plain, Flatpak or Snap) keeps it.
func openPipe(n int) (io.ReadWriteCloser, error) {
	name := fmt.Sprintf("discord-ipc-%d", n)
	var dirs []string
	for _, env := range []string{"XDG_RUNTIME_DIR", "TMPDIR", "TMP", "TEMP"} {
		if d := os.Getenv(env); d != "" {
			dirs = append(dirs, d, filepath.Join(d, "app", "com.discordapp.Discord"), filepath.Join(d, "snap.discord"))
		}
	}
	dirs = append(dirs, "/tmp")
	var last error
	for _, d := range dirs {
		c, err := net.Dial("unix", filepath.Join(d, name))
		if err == nil {
			return c, nil
		}
		last = err
	}
	return nil, last
}
