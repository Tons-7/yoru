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

type ClientImpl interface {
	OpenTorrent(ctx context.Context, info *metainfo.Info, infoHash metainfo.Hash) (TorrentImpl, error)
}

type TorrentCapacity *func() (cap int64, capped bool)

type TorrentImpl struct {
	Piece         func(p metainfo.Piece) PieceImpl
	PieceWithHash func(p metainfo.Piece, pieceHash g.Option[[]byte]) PieceImpl
	Close         func() error
	Capacity      TorrentCapacity

	NewReader      func() TorrentReader
	NewPieceReader func(p Piece) PieceReader
}

type PieceImpl interface {
	io.ReaderAt
	io.WriterAt
	MarkComplete() error
	MarkNotComplete() error
	Completion() Completion
}

type Completion struct {
	Err      error
	Ok       bool
	Complete bool
}

type SelfHashing interface {
	SelfHash() (metainfo.Hash, error)
}

type PieceReaderer interface {
	NewReader() (PieceReader, error)
}

type PieceReader interface {
	io.ReaderAt
	io.Closer
}
