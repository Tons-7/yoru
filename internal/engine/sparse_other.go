//go:build !windows

package engine

import "os"

func markSparse(*os.File) error { return nil }
