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

	"yoru/internal/torrent"
)

// files opens a file per read or write: bt's file storage maps them, and Windows can't delete a mapped file.
type files struct {
	base   string
	pieces storage.PieceCompletion
	// names decides the folder or file name; nil means the torrent's own.
	names *layout
	// vanished hears of a file gone from under complete pieces [first, last], to refresh what bt holds.
	vanished func(hash metainfo.Hash, first, last int)
}

func (s files) OpenTorrent(_ context.Context, info *metainfo.Info, hash metainfo.Hash) (storage.TorrentImpl, error) {
	t := &fileTorrent{info: info, hash: hash, pieces: s.pieces, sparse: map[string]bool{}, vanished: s.vanished}
	top := s.names.top(info.BestName(), hash)
	dir := filepath.Clean(s.base)
	if info.IsDir() {
		dir = filepath.Join(s.base, top)
	}
	for fi := range info.UpvertedFilesIter() {
		path := filepath.Join(dir, top)
		if info.IsDir() {
			path = filepath.Join(append([]string{dir}, safePath(fi.BestPath())...)...)
		}
		if !torrent.Within(s.base, path) {
			return storage.TorrentImpl{}, errors.New("file path escapes the cache folder")
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

// errStorageClosed: a peer's write that lands after its torrent was dropped must not recreate a deleted file.
var errStorageClosed = errors.New("torrent storage closed")

type segment struct {
	path           string
	offset, length int64
	// BEP 47 padding: zeros on the wire, never written to disk.
	padding bool
}

type fileTorrent struct {
	info   *metainfo.Info
	hash   metainfo.Hash
	pieces storage.PieceCompletion
	files  []segment
	// sparse: files marked so on this run, once each.
	mu       sync.Mutex
	sparse   map[string]bool
	closed   atomic.Bool
	vanished func(hash metainfo.Hash, first, last int)
}

// each calls f for every file part of the torrent range [off, off+n).
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

// WriteTo copies the whole piece, opening each file once: hashing would otherwise open it per 32 KiB read.
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
		f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY, 0o644)
		if errors.Is(err, fs.ErrNotExist) {
			if err = os.MkdirAll(filepath.Dir(s.path), 0o755); err == nil {
				f, err = os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY, 0o644)
			}
		}
		if err != nil {
			return err
		}
		fp.t.mu.Lock()
		first := !fp.t.sparse[s.path]
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

// Completion trusts a record only while the bytes are on disk; no bytes is a plain "no", not "unknown".
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

// forgetVanished clears the records of every piece of a file that's gone: once a write regrows it,
// its other pieces' "complete" would serve the zeros of the sparse holes.
func (fp filePiece) forgetVanished() {
	pieceLen := fp.t.info.PieceLength
	fp.t.each(fp.p.Offset(), fp.p.Length(), func(s segment, _, _, _ int64) error {
		if s.padding || s.length == 0 {
			return nil
		}
		if _, err := os.Stat(s.path); err == nil {
			return nil
		}
		first, last := int(s.offset/pieceLen), int((s.offset+s.length-1)/pieceLen)
		for i := first; i <= last; i++ {
			fp.t.pieces.Set(metainfo.PieceKey{InfoHash: fp.t.hash, Index: i}, false)
		}
		fp.t.mu.Lock()
		delete(fp.t.sparse, s.path)
		fp.t.mu.Unlock()
		if fp.t.vanished != nil {
			go fp.t.vanished(fp.t.hash, first, last)
		}
		return nil
	})
}

// onDisk reports whether every file the piece spans is long enough to hold it.
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
