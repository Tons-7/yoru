package disk

import (
	"path/filepath"
	"runtime"
)

func Free(dir string) (bytes int64, drive string, err error) {
	bytes, _, err = space(dir)
	return bytes, Drive(dir), err
}

func Space(dir string) (free, total int64, drive string, err error) {
	free, total, err = space(dir)
	return free, total, Drive(dir), err
}

func Drive(dir string) string {
	if runtime.GOOS == "windows" {
		if v := filepath.VolumeName(dir); v != "" {
			return v
		}
	}
	return dir
}
