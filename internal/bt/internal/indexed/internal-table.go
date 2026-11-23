package indexed

import (
	"iter"

	g "github.com/anacrolix/generics"
	"github.com/anacrolix/missinggo/v2/panicif"
	"yoru/internal/bt/internal/amortize"
	"yoru/internal/bt/internal/check"
)

type table[R Record] struct {
	minRecord     g.Option[R]
	set           btreeSet[R]
	version       int
	cmp           CompareFunc[R]
	indexes       []genericRelation
	indexTriggers []triggerFunc[R]
	triggers      []triggerFunc[R]
	inited        bool
}

func (me *table[R]) GetCmp() CompareFunc[R] {
	return me.cmp
}

func (me *table[R]) SetMinRecord(min R) {
	panicif.GreaterThan(me.cmp(min, me.minRecord.Value), 0)
	me.minRecord.Set(min)
}

func (me *table[R]) Init(cmp func(a, b R) int) {
	panicif.True(me.inited)
	me.set = makeAjwernerSet(cmp)
	me.cmp = cmp
	me.inited = true
}

func (me *table[R]) runTriggers(old, new g.Option[R]) {
	for _, t := range me.triggers {
		t(old, new)
	}
}

func (me *table[R]) OnChange(t triggerFunc[R]) {
	me.triggers = append(me.triggers, t)
}

func (me *table[R]) incVersion() {
	me.version++
}

func (me *table[R]) IterFrom(start R) iter.Seq[R] {
	if me.minRecord.Ok {
		panicif.LessThan(me.cmp(start, me.minRecord.Value), 0)
	}
	return me.set.IterFrom(start)
}

func (me *table[R]) IterFromWhile(gte R, while func(R) bool) iter.Seq[R] {
	return func(yield func(R) bool) {
		for r := range me.IterFrom(gte) {
			if !while(r) || !yield(r) {
				return
			}
		}
	}
}

func (me *table[R]) SelectFirstIf(gte R, filter func(r R) bool) (ret g.Option[R]) {
	for r := range me.IterFromWhile(gte, filter) {
		ret.Set(r)
		break
	}
	return
}

func (me *table[R]) SelectFirstWhere(gte R, where func(r R) bool) (ret g.Option[R]) {
	for r := range me.IterFromWhile(gte, where) {
		ret.Set(r)
		break
	}
	checkWhereGotFirst(me, ret, where)
	return
}

func (me *table[R]) Delete(r R) (removed bool) {
	remK, removed := me.set.Delete(r)
	me.Changed(g.OptionFromTuple(remK, removed), g.None[R]())
	return
}

func (me *table[R]) CreateOrReplace(r R) {
	replaced, overwrote := me.set.Upsert(r)
	me.Changed(g.OptionFromTuple(replaced, overwrote), g.Some(r))
}

func (me *table[R]) Iter(yield func(R) bool) {
	for r := range me.set.Iter {
		if check.Enabled && me.minRecord.Ok && amortize.Try() {
			panicif.LessThan(me.cmp(r, me.minRecord.Value), 0)
		}
		if !yield(r) {
			return
		}
	}
}

func (me *table[R]) Create(r R) (created bool) {
	if me.set.Contains(r) {
		return false
	}
	_, overwrote := me.set.Upsert(r)
	panicif.True(overwrote)
	me.Changed(g.None[R](), g.Some(r))
	return true
}

func (me *table[R]) Update(r R, updateFunc func(r R) R) (existed bool) {
	existed = me.Contains(r)
	if !existed {
		return false
	}
	newRecord := updateFunc(r)
	if newRecord == r {
		return
	}
	replaced, overwrote := me.set.Upsert(newRecord)
	panicif.False(overwrote)
	panicif.NotZero(me.cmp(r, replaced))
	me.Changed(g.Some(r), g.Some(newRecord))
	return true
}

func (me *table[R]) Contains(r R) bool {
	return me.set.Contains(r)
}

func (me *table[R]) Changed(old, new g.Option[R]) {
	me.incVersion()
	if !old.Ok && !new.Ok {
		return
	}
	if old.Ok && new.Ok {
		if old.Value == new.Value {
			return
		}
	}
	for _, t := range me.indexTriggers {
		t(old, new)
	}
	me.runTriggers(old, new)
}

func (me *table[R]) Change(old, new g.Option[R]) {
	if old.Ok {
		if new.Ok {
			_, deleted := me.set.Delete(old.Value)
			panicif.False(deleted)
			_, overwrote := me.set.Upsert(new.Value)
			panicif.True(overwrote)
		} else {
			_, deleted := me.set.Delete(old.Value)
			panicif.False(deleted)
		}
	} else {
		if new.Ok {
			_, overwrote := me.set.Upsert(new.Value)
			panicif.True(overwrote)
		} else {
			return
		}
	}
	me.Changed(old, new)
}

func (me *table[R]) GetFirst() (r R, ok bool) {
	for r = range me.Iter {
		ok = true
		break
	}
	if check.Enabled && amortize.Try() {
		panicif.NotEq(g.OptionFromTuple(r, ok), me.GetGte(me.MinRecord()))
	}
	return
}

func (me *table[R]) GetGte(r R) (ret g.Option[R]) {
	for ret.Value = range me.IterFrom(r) {
		ret.Ok = true
		break
	}
	return
}

func (me *table[R]) Len() int {
	return me.set.Len()
}

func (me *table[R]) MinRecord() (_ R) {
	return me.minRecord.Unwrap()
}
