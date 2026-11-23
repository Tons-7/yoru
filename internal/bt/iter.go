package bt

import (
	"iter"

	"golang.org/x/exp/constraints"
)

func iterRange[T constraints.Integer](start, end T) iter.Seq[T] {
	return func(yield func(T) bool) {
		for i := start; i < end; i++ {
			if !yield(i) {
				return
			}
		}
	}
}
