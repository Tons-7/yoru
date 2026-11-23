package indexed

import (
	g "github.com/anacrolix/generics"
	"github.com/anacrolix/generics/option"
	"github.com/anacrolix/missinggo/v2/panicif"
)

type mapTriggerFunc[K, V any] func(key K, old, new g.Option[V])

type Map[K, V Record] struct {
	Table2[K, V]
	keyCmp func(a, b K) int
}

func (me *Map[K, V]) Init(cmp func(a, b K) int) {
	me.Table2.Init(func(a, b Pair[K, V]) int {
		return cmp(a.Left, b.Left)
	})
	me.keyCmp = cmp
}

type UpdateResult struct {
	Exists  bool
	Changed bool
}

func (me *Map[K, V]) Update(key K, updateFunc func(V) V) (res UpdateResult) {
	start := Pair[K, V]{Left: key}
	gte := me.GetGte(start)
	if !gte.Ok {
		return
	}
	old := gte.Value
	if me.keyCmp(old.Left, key) != 0 {
		return
	}
	res.Exists = true
	new := old
	new.Right = updateFunc(old.Right)
	if new.Right == old.Right {
		return
	}
	replaced, overwrote := me.set.Upsert(new)
	panicif.False(overwrote)
	panicif.NotEq(replaced, old)
	panicif.NotZero(me.cmp(replaced, old))
	res.Changed = true
	me.Changed(g.Some(old), g.Some(new))
	return
}

func (me *Map[K, V]) Alter(key K, updateFunc func(V, bool) (V, bool)) {
	oldV, oldOk := me.Get(key)
	newV, newOk := updateFunc(oldV, oldOk)
	me.Change(
		g.OptionFromTuple(NewPair(key, oldV), oldOk),
		g.OptionFromTuple(NewPair(key, newV), newOk))
}

func (me *Map[K, V]) Get(k K) (v V, ok bool) {
	for r := range me.set.IterFrom(Pair[K, V]{Left: k}) {
		if me.keyCmp(r.Left, k) == 0 {
			v = r.Right
			ok = true
		}
		break
	}
	return
}

func (me *Map[K, V]) ContainsKey(k K) bool {
	_, ok := me.Get(k)
	return ok
}

func (me *Map[K, V]) Delete(k K) (removed bool) {
	return me.Table2.Delete(Pair[K, V]{Left: k})
}

func (me *Map[K, V]) UpdateOrCreate(k K, updateFunc func(old V) V) (created bool) {
	me.Alter(k, func(v V, existed bool) (V, bool) {
		created = !existed
		return updateFunc(v), true
	})
	return
}

func (me *Map[K, V]) OnValueChange(do mapTriggerFunc[K, V]) {
	me.OnChange(func(old, new g.Option[Pair[K, V]]) {
		if old.Ok && new.Ok {
			panicif.NotZero(me.keyCmp(old.Value.Left, new.Value.Left))
		}
		var key K
		if old.Ok {
			key = old.Value.Left
		} else {
			key = new.Unwrap().Left
		}
		do(key, option.Map(pairMapRight, old), option.Map(pairMapRight, new))
	})
}

func (me *Map[K, V]) SetMinRecord(min K) {
	me.table.SetMinRecord(Pair[K, V]{Left: min})
}
