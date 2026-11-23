package indexed

import (
	"iter"

	"github.com/anacrolix/btree"
)

type btreeIterator[R any] struct {
	btree.MapIterator[R, struct{}]
}

type ajwernerBtreeSet[R any] struct {
	inner btree.Set[R]
}

func (me *ajwernerBtreeSet[R]) Delete(r R) (actual R, removed bool) {
	actual, _, removed = me.inner.Map.Delete(r)
	return
}

func (me *ajwernerBtreeSet[R]) Upsert(r R) (_ R, overwrote bool) {
	return me.inner.Upsert(r)
}

func (me *ajwernerBtreeSet[R]) Contains(r R) bool {
	_, ok := me.inner.Get(r)
	return ok
}

func (me *ajwernerBtreeSet[R]) Len() int {
	return me.inner.Len()
}

func (me *ajwernerBtreeSet[R]) getBtreeIterator() btreeIterator[R] {
	return btreeIterator[R]{
		MapIterator: me.inner.Iterator(),
	}
}

func (me *ajwernerBtreeSet[R]) Iter(yield func(R) bool) {
	it := me.getBtreeIterator()
	for it.First(); it.Valid(); it.Next() {
		if !yield(it.Cur()) {
			return
		}
	}
}

func (me *ajwernerBtreeSet[R]) IterFrom(start R) iter.Seq[R] {
	return func(yield func(R) bool) {
		it := me.getBtreeIterator()
		it.SeekGE(start)
		for ; it.Valid(); it.Next() {
			if !yield(it.Cur()) {
				return
			}
		}
	}
}

func makeAjwernerSet[R any](cmp func(R, R) int) *ajwernerBtreeSet[R] {
	return &ajwernerBtreeSet[R]{inner: btree.MakeSet(cmp)}
}
