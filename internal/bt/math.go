package bt

import (
	"golang.org/x/exp/constraints"
)

func intCeilDiv[T constraints.Unsigned](a, b T) T {
	return (a + b - 1) / b
}
