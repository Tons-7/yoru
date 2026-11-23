//go:build !windows && !linux

package disk

import "golang.org/x/sys/unix"

func blockSize(st *unix.Statfs_t) uint64 { return uint64(st.Bsize) }
