package storage

import (
	"context"
	"io"

	g "github.com/anacrolix/generics"

	"yoru/internal/bt/metainfo"
)

type ClientImplCloser interface {
	ClientImpl
	Close() error
}

// Represents data storage for an unspecified torrent.
type ClientImpl interface {
	OpenTorrent(ctx context.Context, info *metainfo.Info, infoHash metainfo.Hash) (TorrentImpl, error)
}

// TorrentCapacity, when set, keys the piece request order; only capped true limits anything.
type TorrentCapacity *func() (cap int64, capped bool)

// Data storage bound to a torrent.
type TorrentImpl struct {
	// v2 infos might not have the piece hash available even if we have the info. The
	// metainfo.Piece.Hash method was removed to enforce this.
	Piece func(p metainfo.Piece) PieceImpl
	// Preferred over PieceWithHash. Called with the piece hash if it's available.
	PieceWithHash func(p metainfo.Piece, pieceHash g.Option[[]byte]) PieceImpl
	Close         func() error
	// Equal pointers for storages sharing one space; called once for the torrents sharing it.
	Capacity TorrentCapacity

	NewReader      func() TorrentReader
	NewPieceReader func(p Piece) PieceReader
}

// PieceImpl is a piece's data. It may also be an io.WriterTo (incomplete chunks written out better),
// SelfHashing (hashes itself better) or PieceReaderer (a better stateful Reader).
type PieceImpl interface {
	// Looser than usual: the arguments always fit the piece.
	io.ReaderAt
	io.WriterAt
	// The data should pass its hash: storage may move it or make it read-only.
	MarkComplete() error
	MarkNotComplete() error
	// The piece's state from a cheap check, sparing a rehash (Torrent caches it).
	Completion() Completion
}

// Completion state of a piece.
type Completion struct {
	Err error
	// The state is known or cached.
	Ok bool
	// If Ok, whether the data is correct. TODO: Check all callsites test Ok first.
	Complete bool
}

// Allows a storage backend to override hashing (i.e. if it can do it more efficiently than the
// torrent client can).
type SelfHashing interface {
	SelfHash() (metainfo.Hash, error)
}

// Piece supports dedicated reader.
type PieceReaderer interface {
	NewReader() (PieceReader, error)
}

type PieceReader interface {
	io.ReaderAt
	io.Closer
}
