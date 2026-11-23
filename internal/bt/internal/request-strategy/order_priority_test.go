package requestStrategy

import (
	"slices"
	"testing"
	"unique"

	"yoru/internal/bt/metainfo"
	"yoru/internal/bt/types"
)

func TestPieceOrderPriorityFirst(t *testing.T) {
	ih := unique.Make(metainfo.Hash{1})
	key := func(i int) PieceRequestOrderKey { return PieceRequestOrderKey{InfoHash: ih, Index: i} }
	order := func(pro *PieceRequestOrder) []int {
		var got []int
		for item := range pro.Iter {
			got = append(got, item.Key.Index)
		}
		return got
	}

	pro := NewPieceOrder(NewAjwernerBtree(), 8)
	pro.Add(key(0), PieceRequestOrderState{Priority: types.PiecePriorityNormal, Partial: true, Availability: 1})
	pro.Add(key(5), PieceRequestOrderState{Priority: types.PiecePriorityReadahead, Availability: 4})
	pro.Add(key(9), PieceRequestOrderState{Priority: types.PiecePriorityNow, Availability: 9})
	if got := order(pro); !slices.Equal(got, []int{9, 5, 0}) {
		t.Fatalf("order = %v, want Now, then Readahead, then Normal", got)
	}

	same := NewPieceOrder(NewAjwernerBtree(), 8)
	same.Add(key(1), PieceRequestOrderState{Priority: types.PiecePriorityNormal, Availability: 1})
	same.Add(key(2), PieceRequestOrderState{Priority: types.PiecePriorityNormal, Partial: true, Availability: 5})
	same.Add(key(3), PieceRequestOrderState{Priority: types.PiecePriorityNormal, Availability: 3})
	if got := order(same); !slices.Equal(got, []int{2, 1, 3}) {
		t.Fatalf("order = %v, want the partial piece first, then rarest first", got)
	}
}
