//go:build !windows && !linux && !darwin

package engine

import "errors"

func punchHole(string, int64, int64) error { return errors.ErrUnsupported }
