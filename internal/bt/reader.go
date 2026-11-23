package bt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/anacrolix/log"
	"github.com/anacrolix/missinggo/v2"
	"github.com/anacrolix/missinggo/v2/panicif"
)

type Reader interface {
	SetContext(context.Context)
	io.ReadSeekCloser
	missinggo.ReadContexter
	SetReadahead(int64)
	SetReadaheadFunc(ReadaheadFunc)
	SetResponsive()
}

type pieceRange struct {
	begin, end pieceIndex
}

type ReadaheadContext struct {
	ContiguousReadStartPos int64
	CurrentPos             int64
}

type ReadaheadFunc func(ReadaheadContext) int64

type reader struct {
	t              *Torrent
	offset, length int64

	storageReader storageReader

	readaheadFunc ReadaheadFunc

	ctx context.Context

	mu sync.Locker

	readahead, pos         int64
	contiguousReadStartPos int64
	pieces                 pieceRange

	reading    bool
	responsive bool
}

func (r *reader) SetContext(ctx context.Context) {
	r.ctx = ctx
}

var _ io.ReadSeekCloser = (*reader)(nil)

func (r *reader) SetResponsive() {
	r.responsive = true
	r.t.cl.event.Broadcast()
}

func (r *reader) SetNonResponsive() {
	r.responsive = false
	r.t.cl.event.Broadcast()
}

func (r *reader) SetReadahead(readahead int64) {
	r.mu.Lock()
	r.readahead = readahead
	r.readaheadFunc = nil
	r.posChanged()
	r.mu.Unlock()
}

func (r *reader) SetReadaheadFunc(f ReadaheadFunc) {
	r.mu.Lock()
	r.readaheadFunc = f
	r.posChanged()
	r.mu.Unlock()
}

func (r *reader) available(off, max int64) (ret int64) {
	off += r.offset
	for max > 0 {
		req, ok := r.t.offsetRequest(off)
		if !ok {
			break
		}
		if !r.responsive && !r.t.pieceComplete(pieceIndex(req.Index)) {
			break
		}
		if !r.t.haveChunk(req) {
			break
		}
		len1 := int64(req.Length) - (off - r.t.requestOffset(req))
		max -= len1
		ret += len1
		off += len1
	}
	if max < 0 {
		ret += max
	}
	return
}

func (r *reader) piecesUncached() (ret pieceRange) {
	ra := r.readahead
	if r.readaheadFunc != nil {
		ra = r.readaheadFunc(ReadaheadContext{
			ContiguousReadStartPos: r.contiguousReadStartPos,
			CurrentPos:             r.pos,
		})
	}
	if ra < 1 {
		ra = 1
	}
	if !r.reading {
		ra = 0
	}
	if ra > r.length-r.pos {
		ra = r.length - r.pos
	}
	ret.begin, ret.end = r.t.byteRegionPieces(r.torrentOffset(r.pos), ra)
	return
}

func (r *reader) Read(b []byte) (n int, err error) {
	return r.read(b)
}

func (r *reader) read(b []byte) (n int, err error) {
	return r.readContext(r.ctx, b)
}

func (r *reader) ReadContext(ctx context.Context, b []byte) (n int, err error) {
	r.ctx = ctx
	return r.Read(b)
}

func (r *reader) readContext(ctx context.Context, b []byte) (n int, err error) {
	if len(b) > 0 {
		r.reading = true
		r.mu.Lock()
		r.posChanged()
		r.mu.Unlock()
	}
	n, err = r.readAt(ctx, b, r.pos)
	if n == 0 {
		if err == nil && len(b) > 0 {
			panic("expected error")
		} else {
			return
		}
	}

	r.mu.Lock()
	r.pos += int64(n)
	r.posChanged()
	r.mu.Unlock()
	if r.pos >= r.length {
		err = io.EOF
	} else if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return
}

var closedChan = make(chan struct{})

func init() {
	close(closedChan)
}

func (r *reader) waitAvailable(
	ctx context.Context,
	pos, wanted int64,
	block bool,
) (avail int64, err error) {
	t := r.t
	for {
		t.cl.rLock()
		avail = r.available(pos, wanted)
		readerCond := t.piece(int((r.offset + pos) / t.info.PieceLength)).readerCond.Signaled()
		t.cl.rUnlock()
		if avail != 0 {
			return
		}
		var dontWait <-chan struct{}
		if !block || wanted == 0 {
			dontWait = closedChan
		}
		select {
		case <-readerCond:
			continue
		case <-r.t.closed.Done():
			err = errTorrentClosed
		case <-ctx.Done():
			err = ctx.Err()
		case <-r.t.dataDownloadDisallowed.On():
			err = errors.New("torrent data downloading disabled")
		case <-r.t.networkingEnabled.Off():
			err = errors.New("torrent networking disabled")
		case <-dontWait:
		}
		return
	}
}

func (r *reader) torrentOffset(readerPos int64) int64 {
	return r.offset + readerPos
}

type waitError struct{ error }

func (e waitError) Unwrap() error { return e.error }

func (r *reader) readOnceAt(ctx context.Context, b []byte, pos int64) (n int, err error) {
	var avail int64
	var b1 []byte
	for {
		avail, err = r.waitAvailable(ctx, pos, int64(len(b)), true)
		if err != nil {
			err = waitError{err}
			return
		}
		if avail == 0 {
			return
		}
		b1 = b[:min(int64(len(b)), avail)]
		r.initStorageReader()
		n, err = r.storageReader.ReadAt(b1, r.torrentOffset(pos))
		if n == 0 {
			break
		}
		r.t.cl.rLock()
		still := r.available(pos, int64(n))
		r.t.cl.rUnlock()
		if still > 0 {
			n = int(still)
			err = nil
			return
		}
	}
	firstPieceIndex := pieceIndex(r.torrentOffset(pos) / r.t.info.PieceLength)
	firstPieceOffset := r.torrentOffset(pos) % r.t.info.PieceLength
	panicif.Nil(err)
	if r.t.closed.IsSet() {
		err = fmt.Errorf("reading from closed torrent: %w", err)
		return
	}
	attrs := [...]any{
		"piece", firstPieceIndex,
		"offset", firstPieceOffset,
		"bytes", len(b1),
		"err", err,
	}
	if r.t.hasStorageCap() {
		r.slogger().Debug("error reading from capped storage", attrs[:]...)
	} else {
		r.slogger().Error("error reading", attrs[:]...)
	}
	return
}

func (r *reader) readAt(ctx context.Context, b []byte, pos int64) (n int, err error) {
	if pos >= r.length {
		err = io.EOF
		return
	}
	defer func() {
		if we, ok := errors.AsType[waitError](err); ok {
			err = we.error
		}
	}()
	n, err = r.readOnceAt(ctx, b, pos)
	if err == nil {
		return
	}
	if _, ok := errors.AsType[waitError](err); ok {
		return
	}
	r.slogger().Error("initial read failed", "err", err)

	err = r.clearStorageReader()
	if err != nil {
		err = fmt.Errorf("closing storage reader after first read failed: %w", err)
		return
	}
	r.storageReader = nil

	n, err = r.readOnceAt(ctx, b, pos)
	if err == nil {
		return
	}
	r.slogger().Error("read failed after reader reset", "err", err)

	r.updatePieceCompletion(pos)

	n, err = r.readOnceAt(ctx, b, pos)
	if err == nil {
		return
	}
	r.slogger().Error("read failed after completion resync", "err", err)

	if r.t.hasStorageCap() {
		return r.readAt(ctx, b, pos)
	}

	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return
}

func (r *reader) updatePieceCompletion(pos int64) {
	firstPieceIndex := pieceIndex(r.torrentOffset(pos) / r.t.info.PieceLength)
	r.t.cl.lock()
	defer r.t.cl.unlock()
	if r.t.closed.IsSet() {
		return
	}
	if !r.t.updatePieceCompletion(firstPieceIndex) {
		r.logger().Levelf(log.Debug, "piece %d completion unchanged", firstPieceIndex)
	}
	if r.pieces.begin != firstPieceIndex {
		panic(fmt.Sprint(r.pieces.begin, firstPieceIndex))
	}
	for index := r.pieces.begin + 1; index < r.pieces.end; index++ {
		r.t.updatePieceCompletion(index)
	}
}

func (r *reader) Close() error {
	r.t.cl.lock()
	r.t.deleteReader(r)
	r.t.cl.unlock()
	return r.clearStorageReader()
}

func (r *reader) posChanged() {
	to := r.piecesUncached()
	from := r.pieces
	if to == from {
		return
	}
	r.pieces = to
	r.t.readerPosChanged(from, to)
}

func (r *reader) Seek(off int64, whence int) (newPos int64, err error) {
	switch whence {
	case io.SeekStart:
		newPos = off
		r.mu.Lock()
	case io.SeekCurrent:
		r.mu.Lock()
		newPos = r.pos + off
	case io.SeekEnd:
		newPos = r.length + off
		r.mu.Lock()
	default:
		return 0, errors.New("bad whence")
	}
	if newPos != r.pos {
		r.reading = false
		r.pos = newPos
		r.contiguousReadStartPos = newPos
		r.posChanged()
	}
	r.mu.Unlock()
	return
}

func (r *reader) logger() log.Logger {
	return r.t.logger
}

func defaultReadaheadFunc(r ReadaheadContext) int64 {
	return r.CurrentPos - r.ContiguousReadStartPos
}

func (r *reader) slogger() *slog.Logger {
	return r.t.slogger()
}

func (r *reader) initStorageReader() {
	if r.storageReader == nil {
		r.storageReader = r.t.storageReader()
	}
}

func (r *reader) clearStorageReader() (err error) {
	if r.storageReader != nil {
		err = r.storageReader.Close()
		if err != nil {
			return
		}
	}
	r.storageReader = nil
	return
}
