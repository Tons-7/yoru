//go:build !windows

package udp

import (
	"errors"
	"strings"
	"syscall"
)

func isErrMessageTooLong(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "message too long")
}

func isErrConnReset(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED)
}
