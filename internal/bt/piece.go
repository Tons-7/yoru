package bt

import (
	"context"
	"fmt"
	"iter"
	"sync"

	"github.com/RoaringBitmap/roaring/v2"
	"github.com/anacrolix/chansync"
	g "github.com/anacrolix/generics"
	"github.com/anacrolix/missinggo/v2/bitmap"
	"github.com/anacrolix/missinggo/v2/panicif"

	"yoru/internal/bt/metainfo"
	pp "yoru/internal/bt/peer_protocol"
	"yoru/internal/bt/storage"
)

type pieceVerifyCount = int64

type Piece struct {
	hash               *metainfo.Hash
	hashV2             g.Option[[32]byte]
	t                  *Torrent
	index              pieceIndex
	beginFile, endFile int

	readerCond chansync.BroadcastCond

	numVerifies     pieceVerifyCount
	numVerifiesCond chansync.BroadcastCond

	publicPieceState     PieceState
	priority             PiecePriority
	relativeAvailability int

	pendingWritesMutex sync.Mutex
	pendingWrites      int
	noPendingWrites    sync.Cond

	dirtiers map[*Peer]struct{}

	race                       byte
	hashing                    bool
	marking                    bool
	storageCompletionOk        bool
	storageCompletionHasBeenOk bool
}

func (p *Piece) String() string {
	return fmt.Sprintf("%s/%d", p.t.canonicalShortInfohash().HexString(), p.index)
}

func (p *Piece) Info() metainfo.Piece {
	return p.t.info.Piece(p.index)
}

func (p *Piece) Storage() storage.Piece {
	var pieceHash g.Option[[]byte]
	if p.hash != nil {
		pieceHash.Set(p.hash.Bytes())
	} else if !p.hasPieceLayer() {
		pieceHash.Set(p.mustGetOnlyFile().piecesRoot.UnwrapPtr()[:])
	} else if p.hashV2.Ok {
		pieceHash.Set(p.hashV2.Value[:])
	}
	return p.t.storage.PieceWithHash(p.Info(), pieceHash)
}

func (p *Piece) pendingChunkIndex(chunkIndex chunkIndexType) bool {
	return !p.chunkIndexDirty(chunkIndex)
}

func (p *Piece) pendingChunk(cs ChunkSpec, chunkSize pp.Integer) bool {
	return p.pendingChunkIndex(chunkIndexFromChunkSpec(cs, chunkSize))
}

func (p *Piece) hasDirtyChunks() bool {
	return p.numDirtyChunks() != 0
}

func (p *Piece) numDirtyChunks() chunkIndexType {
	return chunkIndexType(roaringBitmapRangeCardinality[RequestIndex](
		&p.t.dirtyChunks,
		p.requestIndexBegin(),
		p.t.pieceRequestIndexBegin(p.index+1),
	))
}

func (p *Piece) unpendChunkIndex(i chunkIndexType) {
	p.t.dirtyChunks.Add(p.requestIndexBegin() + i)
	p.t.updatePieceRequestOrderPiece(p.index)
	p.readerCond.Broadcast()
}

func (p *Piece) pendChunkIndex(i RequestIndex) {
	p.t.dirtyChunks.Remove(p.requestIndexBegin() + i)
	p.t.updatePieceRequestOrderPiece(p.index)
}

func (p *Piece) numChunks() chunkIndexType {
	return p.t.pieceNumChunks(p.index)
}

func (p *Piece) incrementPendingWrites() {
	p.pendingWritesMutex.Lock()
	p.pendingWrites++
	p.pendingWritesMutex.Unlock()
}

func (p *Piece) decrementPendingWrites() {
	p.pendingWritesMutex.Lock()
	if p.pendingWrites == 0 {
		panic("assertion")
	}
	p.pendingWrites--
	if p.pendingWrites == 0 {
		p.noPendingWrites.Broadcast()
	}
	p.pendingWritesMutex.Unlock()
}

func (p *Piece) waitNoPendingWrites() {
	p.pendingWritesMutex.Lock()
	for p.pendingWrites != 0 {
		p.noPendingWrites.Wait()
	}
	p.pendingWritesMutex.Unlock()
}

func (p *Piece) chunkIndexDirty(chunk chunkIndexType) bool {
	return p.t.dirtyChunks.Contains(p.requestIndexBegin() + chunk)
}

func (p *Piece) iterCleanChunks(it *roaring.IntIterator) iter.Seq[chunkIndexType] {
	return func(yield func(chunkIndexType) bool) {
		it.Initialize(&p.t.dirtyChunks.Bitmap)
		begin := uint32(p.requestIndexBegin())
		end := uint32(p.requestIndexMaxEnd())
		it.AdvanceIfNeeded(begin)
		for next := begin; next < end; next++ {
			if !it.HasNext() || it.Next() != next {
				if !yield(chunkIndexType(next - begin)) {
					return
				}
			}
		}
		return
	}
}

func (p *Piece) firstCleanChunk() (_ g.Option[chunkIndexType]) {
	for some := range p.iterCleanChunks(&p.t.cl.roaringIntIterator) {
		return g.Some(some)
	}
	return
}

func (p *Piece) chunkIndexSpec(chunk chunkIndexType) ChunkSpec {
	return chunkIndexSpec(pp.Integer(chunk), p.length(), p.chunkSize())
}

func (p *Piece) numDirtyBytes() (ret pp.Integer) {
	numRegularDirtyChunks := p.numDirtyChunks()
	if p.chunkIndexDirty(p.numChunks() - 1) {
		numRegularDirtyChunks--
		ret += p.chunkIndexSpec(p.lastChunkIndex()).Length
	}
	ret += pp.Integer(numRegularDirtyChunks) * p.chunkSize()
	return
}

func (p *Piece) length() pp.Integer {
	return p.t.pieceLength(p.index)
}

func (p *Piece) chunkSize() pp.Integer {
	return p.t.chunkSize
}

func (p *Piece) lastChunkIndex() chunkIndexType {
	return p.numChunks() - 1
}

func (p *Piece) bytesLeft() (ret pp.Integer) {
	if p.t.pieceComplete(p.index) {
		return 0
	}
	return p.length() - p.numDirtyBytes()
}

func (p *Piece) VerifyData() error {
	return p.VerifyDataContext(context.Background())
}

func (p *Piece) VerifyDataContext(ctx context.Context) error {
	locker := p.t.cl.locker()
	locker.Lock()
	if p.t.closed.IsSet() {
		locker.Unlock()
		return errTorrentClosed
	}
	target, err := p.t.queuePieceCheck(p.index)
	locker.Unlock()
	if err != nil {
		return err
	}
	return p.waitVerified(ctx, target)
}

func (p *Piece) waitVerified(ctx context.Context, target pieceVerifyCount) error {
	locker := p.t.cl.locker()
	for {
		locker.RLock()
		done := p.numVerifies >= target
		numVerifiesChanged := p.numVerifiesCond.Signaled()
		locker.RUnlock()
		if done {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.t.closed.Done():
			return errTorrentClosed
		case <-numVerifiesChanged:
		}
	}
}

func (p *Piece) queuedForHash() bool {
	return p.t.piecesQueuedForHash.Contains(p.index)
}

func (p *Piece) torrentBeginOffset() int64 {
	return int64(p.index) * p.t.info.PieceLength
}

func (p *Piece) torrentEndOffset() int64 {
	return p.torrentBeginOffset() + int64(p.t.usualPieceSize())
}

func (p *Piece) SetPriority(prio PiecePriority) {
	p.t.cl.lock()
	defer p.t.cl.unlock()
	p.priority = prio
	p.t.updatePiecePriority(p.index, "Piece.SetPriority")
}

func (p *Piece) purePriority() (ret PiecePriority) {
	for _, f := range p.files() {
		ret.Raise(f.prio)
	}
	if p.t.readerNowPieces().Contains(bitmap.BitIndex(p.index)) {
		ret.Raise(PiecePriorityNow)
	}
	if p.t.readerReadaheadPieces().Contains(bitmap.BitIndex(p.index)) {
		ret.Raise(PiecePriorityReadahead)
	}
	ret.Raise(p.priority)
	return
}

func (p *Piece) ignoreForRequests() bool {

	if !p.storageCompletionOk {
		return true
	}
	if p.hashing || p.marking || !p.haveHash() || p.t.dataDownloadDisallowed.IsSet() {
		return true
	}
	if p.t.pieceComplete(p.index) {
		return true
	}
	if p.queuedForHash() {
		return true
	}
	return false
}

func (p *Piece) effectivePriority() (ret PiecePriority) {
	if p.ignoreForRequests() {
		return PiecePriorityNone
	}
	return p.purePriority()
}

func (p *Piece) UpdateCompletion() {
	p.t.cl.lock()
	defer p.t.cl.unlock()
	p.t.updatePieceCompletion(p.index)
}

func (p *Piece) completion() (ret storage.Completion) {
	ret.Ok = p.storageCompletionOk
	if ret.Ok {
		ret.Complete = p.t.pieceComplete(p.index)
	}
	return
}

func (p *Piece) allChunksDirty() bool {
	return p.numDirtyChunks() == p.numChunks()
}

func (p *Piece) State() PieceState {
	return p.t.PieceState(p.index)
}

func (p *Piece) requestIndexBegin() RequestIndex {
	return p.t.pieceRequestIndexBegin(p.index)
}

func (p *Piece) requestIndexMaxEnd() RequestIndex {
	new := min(p.t.pieceRequestIndexBegin(p.index+1), p.t.maxEndRequest())
	if false {
		old := p.t.pieceRequestIndexBegin(p.index + 1)
		panicif.NotEq(new, old)
	}
	return new
}

func (p *Piece) availability() int {
	return len(p.t.connsWithAllPieces) + p.relativeAvailability
}

func (p *Piece) mustGetOnlyFile() *File {
	panicif.NotEq(p.numFiles(), 1)
	return (*p.t.files)[p.beginFile]
}

func (p *Piece) setV2Hash(v2h [32]byte) {
	p.t.storageLock.Lock()
	oldV2Hash := p.hashV2.Set(v2h)
	p.t.storageLock.Unlock()
	if !oldV2Hash.Ok && p.hash == nil {
		p.t.updatePieceCompletion(p.index)
		p.t.queueInitialPieceCheck(p.index)
	}
}

func (p *Piece) haveHash() bool {
	if p.hash != nil {
		return true
	}
	if !p.hasPieceLayer() {
		return true
	}
	return p.hashV2.Ok
}

func (p *Piece) hasPieceLayer() bool {
	return p.numFiles() == 1 && p.mustGetOnlyFile().length > p.t.info.PieceLength
}

func (p *Piece) files() iter.Seq2[int, *File] {
	return func(yield func(int, *File) bool) {
		for i := p.beginFile; i < p.endFile; i++ {
			if !yield(i, (*p.t.files)[i]) {
				return
			}
		}
	}
}

func (p *Piece) numFiles() int {
	return p.endFile - p.beginFile
}

func (p *Piece) nextNovelHashCount() (ret pieceVerifyCount) {
	ret = p.numVerifies + 1
	if p.hashing {
		ret++
	}
	return
}

func (p *Piece) publishStateChange() {
	t := p.t
	cur := t.pieceState(p.index)
	if cur != p.publicPieceState {
		p.publicPieceState = cur
		t.pieceStateChanges.Publish(PieceStateChange{
			p.index,
			cur,
		})
	}
}
