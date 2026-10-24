// Package disk says how much room the drive holding a folder has left.
package disk

import (
	"path/filepath"
	"runtime"
)

// Free is the space left for this user on the drive holding dir, and the name people know that drive by.
func Free(dir string) (bytes int64, drive string, err error) {
	bytes, err = free(dir)
	return bytes, Drive(dir), err
}

// Drive is "D:" on Windows, else the folder itself, which is what a person would look for.
func Drive(dir string) string {
	if runtime.GOOS == "windows" {
		if v := filepath.VolumeName(dir); v != "" {
			return v
		}
	}
	return dir
}
