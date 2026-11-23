package check

import "testing"

func init() {
	if testing.Testing() {
		Enabled = true
	}
}
