package amortize

import (
	"math/bits"
	"sync/atomic"
)

type Atomic struct {
	herp atomic.Int64
	derp atomic.Int32
}

func (me *Atomic) Try() bool {
	m := me.herp.Add(1)
	shift := me.derp.Load()
	if m >= 1<<shift {
		return me.derp.CompareAndSwap(shift, shift+1)
	}
	return false
}

var global Atomic

func Try() bool {
	return global.Try()
}

type Value struct {
	count uint
}

func (me *Value) Try() bool {
	me.count++
	return bits.OnesCount(me.count) == 1
}
