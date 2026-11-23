//go:build !windows

package library

import (
	"io/fs"
	"syscall"
)

func allocatedSize(_ string, info fs.FileInfo) int64 {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return info.Size()
	}
	return st.Blocks * 512
}
