package dht

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

func ignoreReadFromError(err error) bool {
	if errno, ok := errors.AsType[syscall.Errno](err); ok {
		switch errno {
		case
			windows.WSAENETRESET,
			windows.WSAECONNRESET,
			windows.WSAECONNABORTED,
			windows.WSAECONNREFUSED,
			windows.WSAENETUNREACH,
			windows.WSAETIMEDOUT:
			return true
		}
	}
	return false
}
