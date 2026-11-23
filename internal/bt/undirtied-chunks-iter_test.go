package bt

import (
	"testing"

	typedRoaring "yoru/internal/bt/typed-roaring"
)

func BenchmarkIterUndirtiedRequestIndexesInPiece(b *testing.B) {
	var bitmap typedRoaring.Bitmap[RequestIndex]
	it := bitmap.IteratorType()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		it.Initialize(&bitmap)
		iterBitmapUnsetInRange(&it, 69, 420, func(RequestIndex) {})
	}
}
