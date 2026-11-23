//go:build !windows

package dht

func ignoreReadFromError(error) bool {
	return false
}
