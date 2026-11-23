package segments

import (
	"cmp"
	"iter"
	"slices"

	g "github.com/anacrolix/generics"
	"github.com/anacrolix/missinggo/v2/panicif"
)

func NewIndex(segments LengthIter) (ret Index) {
	var start Length
	for l := range segments {
		ret.segments = append(ret.segments, Extent{start, l})
		start += l
	}
	return
}

type Index struct {
	segments []Extent
}

func NewIndexFromSegments(segments []Extent) Index {
	return Index{segments}
}

func (me Index) iterSegments(startIndex int) iter.Seq[Extent] {
	return func(yield func(Extent) bool) {
		var lastEnd g.Option[Int]
		for _, cur := range me.segments[startIndex:] {
			ret := Extent{
				Start:  cur.Start - lastEnd.UnwrapOr(cur.Start),
				Length: cur.Length,
			}
			lastEnd.Set(cur.End())
			if !yield(ret) {
				return
			}
		}
	}
}

func (me Index) LocateIter(e Extent) iter.Seq2[int, Extent] {
	return func(yield func(int, Extent) bool) {
		first, eq := slices.BinarySearchFunc(me.segments, e.Start, func(elem Extent, target Int) int {
			return cmp.Compare(elem.End(), target+1)
		})
		if first == len(me.segments) {
			return
		}
		_ = eq
		e.Start -= me.segments[first].Start
		if e.Start < 0 {
			e.Length += e.Start
			e.Start = 0
		}
		i := first
		for cons := range scanConsecutive(me.iterSegments(first), e) {
			if !yield(i, cons) {
				return
			}
			i++
		}
	}
}

type IndexAndOffset struct {
	Index  int
	Offset int64
}

func (me Index) LocateOffset(off int64) (ret g.Option[IndexAndOffset]) {
	for i, e := range me.LocateIter(Extent{off, 1}) {
		panicif.True(ret.Ok)
		panicif.NotEq(e.Length, 1)
		ret.Set(IndexAndOffset{
			Index:  i,
			Offset: e.Start,
		})
	}
	return
}

func (me Index) Index(i int) Extent {
	return me.segments[i]
}
