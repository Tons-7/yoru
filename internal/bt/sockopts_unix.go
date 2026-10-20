//go:build !windows && !wasm

package bt

import (
	"syscall"

	"golang.org/x/sys/unix"
)

func setReusePortSockOpts(fd uintptr) (err error) {
	// REUSEPORT alone does on Darwin, likely BSDs and Linux too (libp2p/go-reuseport gets this wrong).
	if false {
		err = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
		if err != nil {
			return
		}
	}
	err = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, unix.SO_REUSEPORT, 1)
	return
}

func setSockNoLinger(fd uintptr) (err error) {
	return syscall.SetsockoptLinger(int(fd), syscall.SOL_SOCKET, syscall.SO_LINGER, &lingerOffVal)
}

func setSockIPTOS(fd uintptr, val int) (err error) {
	return syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_TOS, val)
}
