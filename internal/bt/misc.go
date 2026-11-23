package bt

import (
	"errors"

	"github.com/RoaringBitmap/roaring/v2"
	"github.com/anacrolix/missinggo/v2"
	"github.com/anacrolix/missinggo/v2/panicif"
	"golang.org/x/time/rate"

	"yoru/internal/bt/metainfo"
	pp "yoru/internal/bt/peer_protocol"
	"yoru/internal/bt/types"
	"yoru/internal/bt/types/infohash"
)

type (
	Request       = types.Request
	ChunkSpec     = types.ChunkSpec
	PiecePriority = types.PiecePriority
)

const (
	PiecePriorityNormal    = types.PiecePriorityNormal
	PiecePriorityNone      = types.PiecePriorityNone
	PiecePriorityNow       = types.PiecePriorityNow
	PiecePriorityReadahead = types.PiecePriorityReadahead
	PiecePriorityNext      = types.PiecePriorityNext
	PiecePriorityHigh      = types.PiecePriorityHigh
)

func newRequest(index, begin, length pp.Integer) Request {
	return Request{Index: index, ChunkSpec: ChunkSpec{Begin: begin, Length: length}}
}

func newRequestFromMessage(msg *pp.Message) Request {
	switch msg.Type {
	case pp.Request, pp.Cancel, pp.Reject:
		return newRequest(msg.Index, msg.Begin, msg.Length)
	case pp.Piece:
		return newRequest(msg.Index, msg.Begin, pp.Integer(len(msg.Piece)))
	default:
		panic(msg.Type)
	}
}

func metadataPieceSize(totalSize, piece int) int {
	ret := min(totalSize-piece*(1<<14), 1<<14)
	return ret
}

func torrentOffsetRequest(
	torrentLength, pieceSize, chunkSize, offset int64,
) (
	r Request, ok bool,
) {
	if offset < 0 || offset >= torrentLength {
		return
	}
	r.Index = pp.Integer(offset / pieceSize)
	r.Begin = pp.Integer(offset % pieceSize / chunkSize * chunkSize)
	r.Length = pp.Integer(chunkSize)
	pieceLeft := pp.Integer(pieceSize - int64(r.Begin))
	if r.Length > pieceLeft {
		r.Length = pieceLeft
	}
	torrentLeft := torrentLength - int64(r.Index)*pieceSize - int64(r.Begin)
	if int64(r.Length) > torrentLeft {
		r.Length = pp.Integer(torrentLeft)
	}
	ok = true
	return
}

func torrentRequestOffset(torrentLength, pieceSize int64, r Request) (off int64) {
	off = int64(r.Index)*pieceSize + int64(r.Begin)
	if off < 0 || off >= torrentLength {
		panic("invalid Request")
	}
	return
}

func validateInfo(info *metainfo.Info) error {
	if err := info.Validate(); err != nil {
		return err
	}
	if info.PieceLength < 0 || info.PieceLength > 1<<31 {
		return errors.New("impossible piece length")
	}
	if info.PieceLength == 0 {
		if info.TotalLength() != 0 {
			return errors.New("zero piece length")
		}
	} else if !info.HasV2() {
		if int((info.TotalLength()+info.PieceLength-1)/info.PieceLength) != info.NumPieces() {
			return errors.New("piece count and file lengths are at odds")
		}
	}
	return nil
}

func chunkIndexSpec(index, pieceLength, chunkSize pp.Integer) ChunkSpec {
	begin := index * chunkSize
	panicif.GreaterThanOrEqual(begin, pieceLength)
	return ChunkSpec{
		Begin:  begin,
		Length: min(chunkSize, pieceLength-begin),
	}
}

func comparePeerTrust(l, r *Peer) int {
	return l.trust().Cmp(r.trust())
}

var unlimited = rate.NewLimiter(rate.Inf, 0)

type (
	pieceIndex = int
	InfoHash   = infohash.T
	IpPort     = missinggo.IpPort
)

func boolSliceToBitmap(slice []bool) (rb roaring.Bitmap) {
	for i, b := range slice {
		if b {
			rb.AddInt(i)
		}
	}
	return
}
