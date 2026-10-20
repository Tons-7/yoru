package requestStrategy

import (
	"testing"
	"unique"

	"yoru/internal/bt/metainfo"
)

type fakePiece struct{ available bool }

func (fakePiece) Request() bool                    { return true }
func (fakePiece) CountUnverified() bool            { return false }
func (p fakePiece) Available() bool                { return p.available }
func (t fakeTorrent) Piece(i int) Piece            { return t.pieces[i] }
func (fakeTorrent) PieceLength() int64             { return 1 }
func (in fakeInput) Torrent(metainfo.Hash) Torrent { return in.t }
func (fakeInput) Capacity() (int64, bool)          { return 0, false }
func (fakeInput) MaxUnverifiedBytes() int64        { return 1 }

type fakeTorrent struct{ pieces []fakePiece }
type fakeInput struct{ t fakeTorrent }

// A piece no peer has takes no room in the unverified window, judged by who has it now: the order's cached
// availability can read 0 for a piece every HaveAll seeder holds.
func TestUnavailablePiecesLeaveTheWindowFree(t *testing.T) {
	pro := NewPieceOrder(NewAjwernerBtree(), 3)
	ih := unique.Make(metainfo.Hash{})
	for i := range 3 {
		// All cached at 0, as before any peer connected.
		pro.Add(PieceRequestOrderKey{InfoHash: ih, Index: i}, PieceRequestOrderState{})
	}
	// Piece 0 nobody has; 1 and 2 every seeder has. The window is one piece.
	in := fakeInput{fakeTorrent{[]fakePiece{{false}, {true}, {true}}}}
	var asked []int
	GetRequestablePieces(in, pro, func(_ metainfo.Hash, i int, _ PieceRequestOrderState) bool {
		asked = append(asked, i)
		return true
	})
	if len(asked) != 2 || asked[0] != 0 || asked[1] != 1 {
		t.Errorf("asked %v; want piece 0 (free) then piece 1, which fills the one-piece window", asked)
	}
}
