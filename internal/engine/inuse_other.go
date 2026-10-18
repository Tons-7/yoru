//go:build !windows

package engine

// Unix deletes a file still open; its reader keeps the data until it closes.
func inUse(error) bool { return false }
