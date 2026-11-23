package bt

import (
	"testing"

	qt "github.com/go-quicktest/qt"

	"yoru/internal/bt/metainfo"
)

func TestIssue949LastPieceZeroPadding(t *testing.T) {
	mi, err := metainfo.LoadFromFile("testdata/issue-949.torrent")
	if err != nil {
		panic(err)
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		panic(err)
	}
	lastPiece := info.Piece(info.NumPieces() - 1)
	qt.Assert(t, qt.IsTrue(info.FilesArePieceAligned()))
	qt.Check(t, qt.Equals(lastPiece.V1Length(), info.PieceLength))
	qt.Check(t, qt.Equals(lastPiece.Length(), int64(3677645)))
}
