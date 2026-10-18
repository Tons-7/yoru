//go:build windows

package engine

import (
	"errors"

	"golang.org/x/sys/windows"
)

// inUse: Windows refused the delete because something still has the file open or mapped, which passes.
func inUse(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION) ||
		errors.Is(err, windows.ERROR_USER_MAPPED_FILE)
}
