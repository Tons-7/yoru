package bt

import (
	"errors"

	"golang.org/x/sys/windows"
)

func addrTakenOS(err error) bool {
	return errors.Is(err, windows.WSAEADDRINUSE) || errors.Is(err, windows.WSAEACCES)
}
