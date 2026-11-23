package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	g "github.com/anacrolix/generics"

	"yoru/internal/bt/metainfo"
)

type Client struct {
	ci ClientImpl
}

func NewClient(cl ClientImpl) *Client {
	return &Client{cl}
}

func (cl Client) OpenTorrent(
	ctx context.Context,
	info *metainfo.Info,
	infoHash metainfo.Hash,
) (*Torrent, error) {
	t, err := cl.ci.OpenTorrent(ctx, info, infoHash)
	if err != nil {
		return nil, err
	}
	return &Torrent{t}, nil
}

type Torrent struct {
	TorrentImpl
}

func (t *Torrent) Piece(p metainfo.Piece) Piece {
	return t.PieceWithHash(p, g.Some(p.V1Hash().Unwrap().Bytes()))
}

func (t *Torrent) PieceWithHash(p metainfo.Piece, pieceHash g.Option[[]byte]) Piece {
	var pieceImpl PieceImpl
	if t.TorrentImpl.PieceWithHash != nil {
		pieceImpl = t.TorrentImpl.PieceWithHash(p, pieceHash)
	} else {
		pieceImpl = t.TorrentImpl.Piece(p)
	}
	return Piece{pieceImpl, p}
}

type Piece struct {
	PieceImpl
	mip metainfo.Piece
}

var _ io.WriterTo = Piece{}

func (p Piece) WriteTo(w io.Writer) (_ int64, err error) {
	if i, ok := p.PieceImpl.(io.WriterTo); ok {
		return i.WriteTo(w)
	}
	n := p.mip.Length()
	rc, err := p.NewReader()
	if err != nil {
		err = fmt.Errorf("opening reader: %w", err)
		return
	}
	defer rc.Close()
	r := io.NewSectionReader(rc, 0, n)
	return io.CopyN(w, r, n)
}

func (p Piece) WriteAt(b []byte, off int64) (n int, err error) {
	if off < 0 || off+int64(len(b)) > p.mip.Length() {
		return 0, errors.New("write overflows piece")
	}
	return p.PieceImpl.WriteAt(b, off)
}

func (p Piece) ReadAt(b []byte, off int64) (n int, err error) {
	if off < 0 {
		err = os.ErrInvalid
		return
	}
	if off >= p.mip.Length() {
		err = io.EOF
		return
	}
	b = b[:min(int64(len(b)), p.mip.Length()-off)]
	if len(b) == 0 {
		return
	}
	n, err = p.PieceImpl.ReadAt(b, off)
	if n > len(b) {
		panic(n)
	}
	if n == 0 && err == nil {
		panic("io.Copy will get stuck")
	}
	off += int64(n)

	if off < p.mip.Length() {
		if err == io.EOF {
			p.MarkNotComplete()
		}
	}

	return
}

func (p Piece) NewReader() (PieceReader, error) {
	pr, ok := p.PieceImpl.(PieceReaderer)
	if ok {
		return pr.NewReader()
	}
	return struct {
		io.ReaderAt
		io.Closer
	}{
		p,
		nopCloser{},
	}, nil
}

type nopCloser struct{}

func (nopCloser) Close() error {
	return nil
}
