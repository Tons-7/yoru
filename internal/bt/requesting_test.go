package bt

import (
	"slices"
	"testing"

	g "github.com/anacrolix/generics"
	qt "github.com/go-quicktest/qt"

	requestStrategy "yoru/internal/bt/internal/request-strategy"
	"yoru/internal/bt/metainfo"
	pp "yoru/internal/bt/peer_protocol"
)

func keysAsSlice(m map[Request]struct{}) (sl []Request) {
	for k := range m {
		sl = append(sl, k)
	}
	return
}

func makeTypicalRequests() map[Request]struct{} {
	m := make(map[Request]struct{})
	for p := range pp.Integer(4) {
		for c := range pp.Integer(16) {
			m[Request{Index: p, ChunkSpec: ChunkSpec{Begin: c * defaultChunkSize, Length: defaultChunkSize}}] = struct{}{}
		}
	}
	return m
}

func TestLogExampleRequestMapOrdering(t *testing.T) {
	for k := range makeTypicalRequests() {
		t.Log(k)
	}
}

func TestRequestMapOrderingPersistent(t *testing.T) {
	m := makeTypicalRequests()
	qt.Assert(t, qt.ContentEquals(keysAsSlice(m), keysAsSlice(m)))
}

func TestRequestMapOrderAcrossInstances(t *testing.T) {
	qt.Assert(t, qt.ContentEquals(keysAsSlice(makeTypicalRequests()), keysAsSlice(makeTypicalRequests())))
}

func TestForLoopRepeatItem(t *testing.T) {
	t.Run("ExplicitLoopVar", func(t *testing.T) {
		once := false
		var seen []int
		for i := 0; i < 4; i++ {
			seen = append(seen, i)
			if !once && i == 2 {
				once = true
				i--
				continue
			}
		}
		qt.Assert(t, qt.DeepEquals(seen, []int{0, 1, 2, 2, 3}))
	})
	t.Run("Range", func(t *testing.T) {
		once := false
		var seen []int
		for i := range 4 {
			seen = append(seen, i)
			if !once && i == 2 {
				once = true
				i--
				continue
			}
		}
		qt.Assert(t, qt.DeepEquals(seen, []int{0, 1, 2, 3}))
	})
}

func TestJumpQueueSkipsWhatItCancelled(t *testing.T) {
	cl := newTestingClient(t)
	tor := cl.newTorrentForTesting()
	const pieces, pieceLen = 64, 4 * defaultChunkSize
	qt.Assert(t, qt.IsNil(tor.setInfoUnlocked(&metainfo.Info{
		Pieces: make([]byte, metainfo.HashSize*pieces), Name: "x", PieceLength: pieceLen, Length: pieces * pieceLen,
	})))
	cl.lock()
	defer cl.unlock()
	for i := range pieces {
		tor.piece(i).storageCompletionOk = true
	}
	tor.updateAllPiecePriorities("test")
	for i := range pieces {
		tor.piece(i).priority = PiecePriorityNormal
		tor.updatePiecePriority(i, "test")
	}
	pc := cl.newConnection(nil, newConnectionOpts{network: "test"})
	pc.setTorrent(tor)
	pc.PeerExtensionBytes.SetBit(pp.ExtensionBitFast, true)
	pc.PeerExtensionBytes.SetBit(pp.ExtensionBitLtep, true)
	pc.initMessageWriter()
	qt.Assert(t, qt.IsNil(tor.addPeerConn(pc)))
	pc.onPeerHasAllPiecesNoTriggers()
	pc.peerChoking = false
	perChunks := func(n int) float64 { return float64(n*defaultChunkSize) / RequestQueueTime.Seconds() }

	pc.queue.rate = perChunks(32)
	pc.updateRequests()
	qt.Assert(t, qt.Equals(pc.requestState.Requests.GetCardinality(), 32))
	pc.queue.rate = perChunks(64)
	far := -1
	for i := pieces - 1; i >= 0 && far < 0; i-- {
		asked := false
		for r := range RequestIndex(4) {
			asked = asked || pc.requestState.Requests.Contains(RequestIndex(i)*4+r)
		}
		if !asked {
			far = i
		}
	}
	tor.piece(far).priority = PiecePriorityNow
	tor.updatePiecePriority(far, "test")
	pc.updateRequests()

	qt.Check(t, qt.Equals(pc.requestState.Cancelled.GetCardinality(), 32))
	for r := range RequestIndex(4) {
		qt.Check(t, qt.IsTrue(pc.requestState.Requests.Contains(RequestIndex(far)*4+r)))
	}
	pc.requestState.Requests.Iterate(func(r RequestIndex) bool {
		qt.Check(t, qt.IsFalse(pc.requestState.Cancelled.Contains(r)), qt.Commentf("request %d", r))
		return true
	})
}

func TestNowPieceWakesPeersWithFullQueues(t *testing.T) {
	cl := newTestingClient(t)
	tor := cl.newTorrentForTesting()
	const pieces, pieceLen = 64, 4 * defaultChunkSize
	qt.Assert(t, qt.IsNil(tor.setInfoUnlocked(&metainfo.Info{
		Pieces: make([]byte, metainfo.HashSize*pieces), Name: "x", PieceLength: pieceLen, Length: pieces * pieceLen,
	})))
	cl.lock()
	defer cl.unlock()
	for i := range pieces {
		tor.piece(i).storageCompletionOk = true
	}
	tor.updateAllPiecePriorities("test")
	for i := range pieces {
		tor.piece(i).priority = PiecePriorityNormal
		tor.updatePiecePriority(i, "test")
	}
	pc := cl.newConnection(nil, newConnectionOpts{network: "test"})
	pc.setTorrent(tor)
	pc.PeerExtensionBytes.SetBit(pp.ExtensionBitFast, true)
	pc.PeerExtensionBytes.SetBit(pp.ExtensionBitLtep, true)
	pc.initMessageWriter()
	qt.Assert(t, qt.IsNil(tor.addPeerConn(pc)))
	pc.onPeerHasAllPiecesNoTriggers()
	pc.peerChoking = false
	pc.queue.rate = float64(32*defaultChunkSize) / RequestQueueTime.Seconds()
	pc.updateRequests()
	qt.Assert(t, qt.IsFalse(pc.isLowOnRequests()))

	requested := func(i int) bool {
		for r := range RequestIndex(4) {
			if pc.requestState.Requests.Contains(RequestIndex(i)*4 + r) {
				return true
			}
		}
		return false
	}
	var asked, unasked []int
	for i := range pieces {
		if requested(i) {
			asked = append(asked, i)
		} else {
			unasked = append(unasked, i)
		}
	}
	qt.Assert(t, qt.IsTrue(len(asked) > 0 && len(unasked) > 2))
	raise := func(i int, to PiecePriority) string {
		pc.needRequestUpdate = ""
		tor.piece(i).priority = to
		tor.updatePiecePriority(i, "test")
		return string(pc.needRequestUpdate)
	}

	qt.Check(t, qt.Equals(raise(unasked[0], PiecePriorityReadahead), ""), qt.Commentf("a non-urgent change woke a full peer"))
	qt.Check(t, qt.Not(qt.Equals(raise(unasked[1], PiecePriorityNow), "")), qt.Commentf("a Now piece nobody asked for didn't wake the peer"))
	pc.peerChoking = true
	qt.Check(t, qt.Equals(raise(unasked[2], PiecePriorityNow), ""), qt.Commentf("a choking peer was woken"))
}

func TestNowPiecesAskedFromTheReaderOn(t *testing.T) {
	cl := newTestingClient(t)
	tor := cl.newTorrentForTesting()
	tor._chunksPerRegularPiece = 4
	pc := cl.newConnection(nil, newConnectionOpts{network: "test"})
	pc.setTorrent(tor)
	states := make([]g.Option[requestStrategy.PieceRequestOrderState], 2)
	for i := range states {
		states[i].Set(requestStrategy.PieceRequestOrderState{Priority: PiecePriorityNow})
	}
	d := desiredPeerRequests{peer: pc, pieceStates: states, readerAt: []RequestIndex{2, 5}}
	got := []RequestIndex{7, 0, 5, 3, 1, 6, 2, 4}
	slices.SortFunc(got, func(a, b RequestIndex) int {
		if d.lessByValue(a, b) {
			return -1
		}
		if d.lessByValue(b, a) {
			return 1
		}
		return 0
	})
	qt.Assert(t, qt.DeepEquals(got, []RequestIndex{2, 3, 0, 1, 5, 6, 7, 4}))
}
