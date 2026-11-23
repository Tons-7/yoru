package engine

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"yoru/internal/bt/metainfo"
	"yoru/internal/bt/storage"
)

type files struct {
	base     string
	pieces   storage.PieceCompletion
	names    *layout
	vanished func(hash metainfo.Hash, first, last int)
}

func (s files) OpenTorrent(_ context.Context, info *metainfo.Info, hash metainfo.Hash) (storage.TorrentImpl, error) {
	t := &fileTorrent{base: s.base, info: info, hash: hash, pieces: s.pieces, sparse: map[string]bool{}, vanished: s.vanished}
	top := s.names.top(info.BestName(), hash)
	dir := filepath.Clean(s.base)
	if info.IsDir() {
		dir = filepath.Join(s.base, top)
	}
	for fi := range info.UpvertedFilesIter() {
		path := filepath.Join(dir, top)
		if info.IsDir() {
			path = filepath.Join(append([]string{dir}, s.names.parts(info, hash, fi)...)...)
		}
		if !inTorrent(s.base, top, info.IsDir(), path) {
			return storage.TorrentImpl{}, errors.New("file path escapes the torrent's folder")
		}
		t.files = append(t.files, segment{
			path: path, offset: fi.TorrentOffset, length: fi.Length,
			padding: strings.Contains(fi.Attr, "p"),
		})
	}
	return storage.TorrentImpl{
		Piece: func(p metainfo.Piece) storage.PieceImpl { return filePiece{t, p} },
		Close: func() error { t.closed.Store(true); return nil },
	}, nil
}

var errStorageClosed = errors.New("torrent storage closed")

type segment struct {
	path           string
	offset, length int64
	padding        bool
}

type fileTorrent struct {
	base     string
	info     *metainfo.Info
	hash     metainfo.Hash
	pieces   storage.PieceCompletion
	files    []segment
	mu       sync.Mutex
	sparse   map[string]bool
	closed   atomic.Bool
	vanished func(hash metainfo.Hash, first, last int)
}

func (t *fileTorrent) each(off, n int64, f func(s segment, fileOff, bufOff, length int64) error) error {
	end := off + n
	for _, s := range t.files {
		lo, hi := max(off, s.offset), min(end, s.offset+s.length)
		if lo >= hi {
			continue
		}
		if err := f(s, lo-s.offset, lo-off, hi-lo); err != nil {
			return err
		}
	}
	return nil
}

type filePiece struct {
	t *fileTorrent
	p metainfo.Piece
}

func (fp filePiece) key() metainfo.PieceKey {
	return metainfo.PieceKey{InfoHash: fp.t.hash, Index: fp.p.Index()}
}

func (fp filePiece) ReadAt(b []byte, off int64) (int, error) {
	base := fp.p.Offset() + off
	err := fp.t.each(base, int64(len(b)), func(s segment, fileOff, bufOff, n int64) error {
		dst := b[bufOff : bufOff+n]
		if s.padding {
			clear(dst)
			return nil
		}
		f, err := os.Open(s.path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = f.ReadAt(dst, fileOff)
		if errors.Is(err, io.EOF) {
			return io.ErrUnexpectedEOF
		}
		return err
	})
	if err != nil {
		return 0, err
	}
	return len(b), nil
}

func (fp filePiece) WriteTo(w io.Writer) (int64, error) {
	var total int64
	err := fp.t.each(fp.p.Offset(), fp.p.Length(), func(s segment, fileOff, _, n int64) error {
		if s.padding {
			c, err := io.CopyN(w, zeros{}, n)
			total += c
			return err
		}
		f, err := os.Open(s.path)
		if err != nil {
			return err
		}
		defer f.Close()
		c, err := io.Copy(w, io.NewSectionReader(f, fileOff, n))
		total += c
		if err == nil && c < n {
			err = io.ErrUnexpectedEOF
		}
		return err
	})
	return total, err
}

type zeros struct{}

func (zeros) Read(b []byte) (int, error) {
	clear(b)
	return len(b), nil
}

func (fp filePiece) WriteAt(b []byte, off int64) (int, error) {
	base := fp.p.Offset() + off
	err := fp.t.each(base, int64(len(b)), func(s segment, fileOff, bufOff, n int64) error {
		if s.padding {
			return nil
		}
		if fp.t.closed.Load() {
			return errStorageClosed
		}
		f, err := os.OpenFile(s.path, os.O_WRONLY, 0)
		created := false
		if errors.Is(err, fs.ErrNotExist) {
			created = true
			f, err = os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY, 0o644)
			if errors.Is(err, fs.ErrNotExist) {
				if err = os.MkdirAll(filepath.Dir(s.path), 0o755); err == nil {
					f, err = os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY, 0o644)
				}
			}
		}
		if err != nil {
			return err
		}
		if created && fp.t.closed.Load() {
			f.Close()
			os.Remove(s.path)
			return errStorageClosed
		}
		fp.t.mu.Lock()
		first := created || !fp.t.sparse[s.path]
		fp.t.sparse[s.path] = true
		fp.t.mu.Unlock()
		if first {
			markSparse(f)
		}
		_, werr := f.WriteAt(b[bufOff:bufOff+n], fileOff)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		return werr
	})
	if err != nil {
		return 0, err
	}
	return len(b), nil
}

func (fp filePiece) MarkComplete() error    { return fp.t.pieces.Set(fp.key(), true) }
func (fp filePiece) MarkNotComplete() error { return fp.t.pieces.Set(fp.key(), false) }

func (fp filePiece) Completion() storage.Completion {
	c, err := fp.t.pieces.Get(fp.key())
	if err != nil || (c.Ok && !c.Complete) {
		return c
	}
	if !fp.onDisk() {
		if c.Ok && c.Complete {
			fp.forgetVanished()
		}
		return storage.Completion{Ok: true, Complete: false}
	}
	return c
}

func (fp filePiece) forgetVanished() {
	pieceLen := fp.t.info.PieceLength
	fp.t.each(fp.p.Offset(), fp.p.Length(), func(s segment, _, _, _ int64) error {
		if s.padding || s.length == 0 {
			return nil
		}
		var size int64
		st, err := os.Stat(s.path)
		switch {
		case err == nil && st.Size() >= s.length:
			return nil
		case err == nil:
			size = st.Size()
		case !errors.Is(err, fs.ErrNotExist) || !fp.t.baseThere():
			return nil
		}
		first, last := int((s.offset+size)/pieceLen), int((s.offset+s.length-1)/pieceLen)
		for i := first; i <= last; i++ {
			fp.t.pieces.Set(metainfo.PieceKey{InfoHash: fp.t.hash, Index: i}, false)
		}
		if err != nil {
			fp.t.mu.Lock()
			delete(fp.t.sparse, s.path)
			fp.t.mu.Unlock()
		}
		if fp.t.vanished != nil {
			go fp.t.vanished(fp.t.hash, first, last)
		}
		return nil
	})
}

func (t *fileTorrent) baseThere() bool {
	_, err := os.Stat(t.base)
	return err == nil
}

func (fp filePiece) onDisk() bool {
	ok := true
	fp.t.each(fp.p.Offset(), fp.p.Length(), func(s segment, fileOff, _, n int64) error {
		if s.padding {
			return nil
		}
		if info, err := os.Stat(s.path); err != nil || info.Size() < fileOff+n {
			ok = false
		}
		return nil
	})
	return ok
}
