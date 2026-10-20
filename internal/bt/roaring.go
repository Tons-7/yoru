package bt

import typedRoaring "yoru/internal/bt/typed-roaring"

// The bits set in [start, end): the last item's rank less the rank before the first (mind the off-by-ones).
func roaringBitmapRangeCardinality[T typedRoaring.BitConstraint](bm interface{ Rank(T) uint64 }, start, end T) (card uint64) {
	card = bm.Rank(end - 1)
	if start != 0 {
		card -= bm.Rank(start - 1)
	}
	return
}
