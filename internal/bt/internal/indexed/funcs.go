package indexed

import (
	"fmt"
	"iter"

	g "github.com/anacrolix/generics"
	"yoru/internal/bt/internal/amortize"
	"yoru/internal/bt/internal/check"
)

func IterClusteredWhere[R any](t relation[R], gte R, where func(r R) bool) Iter[R] {
	return func(yield func(R) bool) {
		first := true
		checkFirst := func(r g.Option[R]) {
			if !first {
				return
			}
			checkWhereGotFirst(t, r, where)
			first = false
		}
		for r := range t.IterFrom(gte) {
			if !where(r) {
				break
			}
			checkFirst(g.Some(r))
			if !yield(r) {
				return
			}
		}
		checkFirst(g.None[R]())
	}
}

func checkWhereGotFirst[R any](me relation[R], first g.Option[R], where func(r R) bool) {
	if !check.Enabled || !amortize.Try() {
		return
	}
	var slowRet g.Option[R]
	for r := range me.Iter {
		if where(r) {
			slowRet.Set(r)
			break
		}
	}
	if first.Ok != slowRet.Ok || first.Ok && me.GetCmp()(first.Value, slowRet.Value) != 0 {
		fmt.Printf("%#v\n", first.Value)
		fmt.Printf("%#v\n", slowRet.Value)
		panic("iterating where got different first value than caller")
	}
}

func IterRange[R any](me relation[R], gte, lt R) iter.Seq[R] {
	return func(yield func(R) bool) {
		for r := range me.IterFrom(gte) {
			if me.GetCmp()(r, lt) >= 0 {
				break
			}
			if !yield(r) {
				break
			}
		}
	}
}
