package bt

import (
	"iter"

	"github.com/RoaringBitmap/roaring/v2"
	g "github.com/anacrolix/generics"
	"github.com/anacrolix/missinggo/v2/bitmap"

	"yoru/internal/bt/metainfo"
	infohash_v2 "yoru/internal/bt/types/infohash-v2"
)

type File struct {
	t           *Torrent
	path        string
	offset      int64
	length      int64
	fi          metainfo.FileInfo
	displayPath string
	prio        PiecePriority
	piecesRoot  g.Option[infohash_v2.T]
}

func (f *File) String() string {
	return f.Path()
}

func (f *File) Torrent() *Torrent {
	return f.t
}

func (f *File) Offset() int64 {
	return f.offset
}

func (f *File) FileInfo() metainfo.FileInfo {
	return f.fi
}

func (f *File) Path() string {
	return f.path
}

func (f *File) Length() int64 {
	return f.length
}

func (f *File) BytesCompleted() (n int64) {
	f.t.cl.rLock()
	n = f.bytesCompletedLocked()
	f.t.cl.rUnlock()
	return
}

func (f *File) bytesCompletedLocked() int64 {
	return f.length - f.bytesLeft()
}

func fileBytesLeft(
	torrentUsualPieceSize int64,
	fileFirstPieceIndex int,
	fileEndPieceIndex int,
	fileTorrentOffset int64,
	fileLength int64,
	torrentCompletedPieces *roaring.Bitmap,
	pieceSizeCompletedFn func(pieceIndex int) int64,
) (left int64) {
	if fileLength == 0 {
		return
	}

	noCompletedMiddlePieces := roaring.New()
	noCompletedMiddlePieces.AddRange(bitmap.BitRange(fileFirstPieceIndex), bitmap.BitRange(fileEndPieceIndex))
	noCompletedMiddlePieces.AndNot(torrentCompletedPieces)
	noCompletedMiddlePieces.Iterate(func(pieceIndex uint32) bool {
		i := int(pieceIndex)
		pieceSizeCompleted := pieceSizeCompletedFn(i)
		if i == fileFirstPieceIndex {
			beginOffset := fileTorrentOffset % torrentUsualPieceSize
			beginSize := torrentUsualPieceSize - beginOffset
			beginDownLoaded := max(pieceSizeCompleted-beginOffset, 0)
			left += beginSize - beginDownLoaded
		} else if i == fileEndPieceIndex-1 {
			endSize := (fileTorrentOffset + fileLength) % torrentUsualPieceSize
			if endSize == 0 {
				endSize = torrentUsualPieceSize
			}
			endDownloaded := min(pieceSizeCompleted, endSize)
			left += endSize - endDownloaded
		} else {
			left += torrentUsualPieceSize - pieceSizeCompleted
		}
		return true
	})

	if left > fileLength {
		left = fileLength
	}
	return
}

func (f *File) bytesLeft() (left int64) {
	return fileBytesLeft(
		int64(f.t.usualPieceSize()),
		f.BeginPieceIndex(),
		f.EndPieceIndex(),
		f.offset,
		f.length,
		&f.t._completedPieces,
		func(pieceIndex int) int64 {
			return int64(f.t.piece(pieceIndex).numDirtyBytes())
		},
	)
}

func (f *File) DisplayPath() string {
	return f.displayPath
}

type FilePieceState struct {
	Bytes int64
	PieceState
}

func (f *File) State() (ret []FilePieceState) {
	f.t.cl.rLock()
	defer f.t.cl.rUnlock()
	pieceSize := int64(f.t.usualPieceSize())
	off := f.offset % pieceSize
	remaining := f.length
	for i := pieceIndex(f.offset / pieceSize); ; i++ {
		if remaining == 0 {
			break
		}
		len1 := min(pieceSize-off, remaining)
		ps := f.t.pieceState(i)
		ret = append(ret, FilePieceState{len1, ps})
		off = 0
		remaining -= len1
	}
	return
}

func (f *File) Download() {
	f.SetPriority(PiecePriorityNormal)
}

func byteRegionExclusivePieces(off, size, pieceSize int64) (begin, end int) {
	begin = int((off + pieceSize - 1) / pieceSize)
	end = int((off + size) / pieceSize)
	return
}

func (f *File) NewReader() Reader {
	return f.t.newReader(f.Offset(), f.Length())
}

func (f *File) SetPriority(prio PiecePriority) {
	f.t.cl.lock()
	if prio != f.prio {
		f.prio = prio
		f.t.updatePiecePriorities(f.BeginPieceIndex(), f.EndPieceIndex(), "File.SetPriority")
	}
	f.t.cl.unlock()
}

func (f *File) Priority() (prio PiecePriority) {
	f.t.cl.rLock()
	prio = f.prio
	f.t.cl.rUnlock()
	return
}

func (f *File) BeginPieceIndex() int {
	return f.fi.BeginPieceIndex(int64(f.t.usualPieceSize()))
}

func (f *File) EndPieceIndex() int {
	return f.fi.EndPieceIndex(int64(f.t.usualPieceSize()))
}

func (f *File) numPieces() int {
	return f.EndPieceIndex() - f.BeginPieceIndex()
}

func (f *File) PieceIndices() iter.Seq[int] {
	return func(yield func(int) bool) {
		for i := f.BeginPieceIndex(); i < f.EndPieceIndex(); i++ {
			if !yield(i) {
				break
			}
		}
	}
}

func (f *File) Pieces() iter.Seq[*Piece] {
	return func(yield func(*Piece) bool) {
		for i := range f.PieceIndices() {
			p := f.t.piece(i)
			if !yield(p) {
				break
			}
		}
	}
}
