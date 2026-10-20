package discord

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/windows"
)

// openPipe opens the Discord app's named pipe n.
func openPipe(n int) (io.ReadWriteCloser, error) {
	return openNamedPipe(fmt.Sprintf(`\\.\pipe\discord-ipc-%d`, n))
}

// openNamedPipe opens it overlapped: a blocking handle runs one call at a time, so reads held writes up.
func openNamedPipe(name string) (io.ReadWriteCloser, error) {
	return os.OpenFile(name, os.O_RDWR|windows.O_FILE_FLAG_OVERLAPPED, 0)
}
