//go:build !windows

package proc

import (
	"os"
	"syscall"
)

// Lock holds an exclusive lock on path for the process's life; a second Yoru on the same data gets an error.
func Lock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}

// Alert: a terminal keeps the message on screen; nothing more to do.
func Alert(string, string) {}
