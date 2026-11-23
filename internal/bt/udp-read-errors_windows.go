package bt

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

func transientUDPReadError(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	switch errno {
	case windows.WSAEMSGSIZE, windows.WSAENETRESET, windows.WSAECONNRESET, windows.WSAECONNABORTED,
		windows.WSAECONNREFUSED, windows.WSAENETUNREACH, windows.WSAETIMEDOUT:
		return true
	}
	return false
}
