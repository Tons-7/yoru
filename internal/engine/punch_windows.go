//go:build windows

package engine

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func punchHole(path string, offset, length int64) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := markSparse(f); err != nil {
		return err
	}
	zero := struct{ FileOffset, BeyondFinalZero int64 }{offset, offset + length}
	var returned uint32
	return windows.DeviceIoControl(windows.Handle(f.Fd()), windows.FSCTL_SET_ZERO_DATA,
		(*byte)(unsafe.Pointer(&zero)), uint32(unsafe.Sizeof(zero)), nil, 0, &returned, nil)
}
