package containers

import (
	"github.com/anacrolix/stm/stmutil"

	"yoru/internal/bt/dht/int160"
	"yoru/internal/bt/dht/types"
)

type addrMaybeId = types.AddrMaybeId

type AddrMaybeIdsByDistance interface {
	Add(addrMaybeId) AddrMaybeIdsByDistance
	Next() addrMaybeId
	Delete(addrMaybeId) AddrMaybeIdsByDistance
	Len() int
}

type stmSettishWrapper struct {
	set stmutil.Settish[addrMaybeId]
}

// Next is the closest; callers check Len first, as an empty set has none.
func (me stmSettishWrapper) Next() addrMaybeId {
	for x := range me.set.All() {
		return x
	}
	panic("Next on an empty set")
}

func (me stmSettishWrapper) Delete(x addrMaybeId) AddrMaybeIdsByDistance {
	return stmSettishWrapper{me.set.Delete(x)}
}

func (me stmSettishWrapper) Len() int {
	return me.set.Len()
}

func (me stmSettishWrapper) Add(x addrMaybeId) AddrMaybeIdsByDistance {
	return stmSettishWrapper{me.set.Add(x)}
}

func NewImmutableAddrMaybeIdsByDistance(target int160.T) AddrMaybeIdsByDistance {
	return stmSettishWrapper{stmutil.NewSortedSet[addrMaybeId](func(l, r addrMaybeId) bool {
		return l.CloserThan(r, target)
	})}
}
