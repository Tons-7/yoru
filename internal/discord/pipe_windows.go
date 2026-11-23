package discord

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/windows"
)

func openPipe(n int) (io.ReadWriteCloser, error) {
	return openNamedPipe(fmt.Sprintf(`\\.\pipe\discord-ipc-%d`, n))
}

func openNamedPipe(name string) (io.ReadWriteCloser, error) {
	return os.OpenFile(name, os.O_RDWR|windows.O_FILE_FLAG_OVERLAPPED, 0)
}
