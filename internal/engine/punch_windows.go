//go:build windows

package engine

import (
	"errors"
	"fmt"
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
		return unsupported(err)
	}
	zero := struct{ FileOffset, BeyondFinalZero int64 }{offset, offset + length}
	var returned uint32
	return unsupported(windows.DeviceIoControl(windows.Handle(f.Fd()), windows.FSCTL_SET_ZERO_DATA,
		(*byte)(unsafe.Pointer(&zero)), uint32(unsafe.Sizeof(zero)), nil, 0, &returned, nil))
}

func unsupported(err error) error {
	if errors.Is(err, windows.ERROR_INVALID_FUNCTION) || errors.Is(err, windows.ERROR_NOT_SUPPORTED) {
		return fmt.Errorf("%w: %v", errors.ErrUnsupported, err)
	}
	return err
}
