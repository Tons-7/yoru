//go:build windows

package engine

import (
	"errors"

	"golang.org/x/sys/windows"
)

func inUse(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION) ||
		errors.Is(err, windows.ERROR_USER_MAPPED_FILE)
}

func badName(err error) bool {
	return errors.Is(err, windows.ERROR_FILENAME_EXCED_RANGE) || errors.Is(err, windows.ERROR_INVALID_NAME)
}
