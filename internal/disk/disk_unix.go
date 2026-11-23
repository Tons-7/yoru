//go:build !windows

package disk

import "golang.org/x/sys/unix"

func space(dir string) (int64, int64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, 0, err
	}
	size := blockSize(&st)
	return int64(uint64(st.Bavail) * size), int64((uint64(st.Blocks) - uint64(st.Bfree) + uint64(st.Bavail)) * size), nil
}
