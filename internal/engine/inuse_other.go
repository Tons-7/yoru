//go:build !windows

package engine

import (
	"errors"
	"syscall"
)

func inUse(error) bool { return false }

func badName(err error) bool {
	return errors.Is(err, syscall.ENAMETOOLONG) || errors.Is(err, syscall.EILSEQ)
}
