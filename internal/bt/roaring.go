package bt

import typedRoaring "yoru/internal/bt/typed-roaring"

func roaringBitmapRangeCardinality[T typedRoaring.BitConstraint](bm interface{ Rank(T) uint64 }, start, end T) (card uint64) {
	if end <= start {
		return 0
	}
	card = bm.Rank(end - 1)
	if start != 0 {
		card -= bm.Rank(start - 1)
	}
	return
}
