//go:build !windows

package transcode

import "os"

func openShared(path string) (*os.File, error) { return os.Open(path) }
