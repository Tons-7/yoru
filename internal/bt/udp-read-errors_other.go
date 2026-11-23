//go:build !windows

package bt

import (
	"errors"
	"syscall"
)

func transientUDPReadError(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED)
}
