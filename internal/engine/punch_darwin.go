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
	var fs unix.Statfs_t
	if err := unix.Fstatfs(int(f.Fd()), &fs); err != nil {
		return err
	}
	if bs := int64(fs.Bsize); bs > 0 {
		end := (offset + length) / bs * bs
		offset = (offset + bs - 1) / bs * bs
		if end <= offset {
			return nil
		}
		length = end - offset
	}
	hole := struct {
		flags, reserved uint32
		offset, length  int64
	}{offset: offset, length: length}
	if _, _, errno := syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), uintptr(unix.F_PUNCHHOLE), uintptr(unsafe.Pointer(&hole))); errno != 0 {
		return errno
	}
	return nil
}
