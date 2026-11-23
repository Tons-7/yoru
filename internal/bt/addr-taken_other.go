//go:build !windows

package bt

func addrTakenOS(error) bool { return false }
