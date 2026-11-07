//go:build darwin

package engine

import (
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

func punchHole(path string, offset, length int64) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	hole := struct {
		flags, reserved uint32
		offset, length  int64
	}{offset: offset, length: length}
	if _, _, errno := syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), uintptr(unix.F_PUNCHHOLE), uintptr(unsafe.Pointer(&hole))); errno != 0 {
		return errno
	}
	return nil
}
