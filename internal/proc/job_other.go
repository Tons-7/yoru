//go:build !windows

package proc

func KillChildrenOnExit() error { return nil }
