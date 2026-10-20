package bt

import (
	"bytes"
	"math"
	"net/netip"
	"os"
	"testing"

	"github.com/go-quicktest/qt"

	"yoru/internal/bt/internal/testutil"
	pp "yoru/internal/bt/peer_protocol"
	utHolepunch "yoru/internal/bt/peer_protocol/ut-holepunch"
	"yoru/internal/bt/storage"
)

// A peer on a torrent: one without info yet (a magnet resolving), or one with a small v1 info.
func hostilePeer(t *testing.T, withInfo bool) (*Client, *Torrent, *PeerConn) {
	cl := newTestingClient(t)
	var tor *Torrent
	if withInfo {
		dir, mi := testutil.GreetingTestTorrent()
		t.Cleanup(func() { os.RemoveAll(dir) })
		tor = cl.newTorrent(mi.HashInfoBytes(), storage.NewFileWithCompletion(t.TempDir(), storage.NewMapPieceCompletion()))
		tor.setChunkSize(2)
		cl.lock()
		qt.Assert(t, qt.IsNil(tor.setInfoBytesLocked(mi.InfoBytes)))
		cl.unlock()
	} else {
		tor = cl.newTorrentForTesting()
	}
	pc := cl.newConnection(nil, newConnectionOpts{network: "test"})
	pc.setTorrent(tor)
	pc.initMessageWriter()
	return cl, tor, pc
}

// Each of these was one message from any peer that took the whole process down.
func TestHostilePeerMessagesAreErrorsNotPanics(t *testing.T) {
	for _, withInfo := range []bool{false, true} {
		cl, tor, pc := hostilePeer(t, withInfo)
		cl.lock()
		hashes := pp.Message{Type: pp.Hashes, Index: math.MaxUint32, Length: 1, Hashes: [][32]byte{{}}}
		qt.Check(t, qt.IsNotNil(pc.onReadHashes(&hashes)))
		req := pp.Message{Type: pp.HashRequest, Index: math.MaxUint32, Length: 512}
		qt.Check(t, qt.IsNotNil(pc.onHashRequest(&req)))
		// Data for a piece out of range is an error; a request for any piece is rejected politely, as peers
		// ask before we know the size, and must never index what we hold.
		qt.Check(t, qt.IsNotNil(cl.gotMetadataExtensionMsg([]byte("d8:msg_typei1e5:piecei-1ee"), tor, pc)))
		for _, payload := range []string{"d8:msg_typei0e5:piecei-1ee", "d8:msg_typei0e5:piecei99ee"} {
			qt.Check(t, qt.IsNil(cl.gotMetadataExtensionMsg([]byte(payload), tor, pc)))
		}
		cl.unlock()
	}
}

func TestMetadataSizeFromPeerIsBounded(t *testing.T) {
	_, tor, _ := hostilePeer(t, false)
	for _, size := range []int{-1, -4294966296, 1<<32 + 1000, int(maxMetadataSize) + 1} {
		qt.Check(t, qt.IsNotNil(tor.setMetadataSize(size)), qt.Commentf("size %d", size))
	}
	qt.Check(t, qt.HasLen(tor.metadataBytes, 0))
	qt.Assert(t, qt.IsNil(tor.setMetadataSize(1000)))
	qt.Check(t, qt.HasLen(tor.metadataBytes, 1000))
}

// A wrong first size is corrected by the next peer's, and the whole chunks already in survive it.
func TestMetadataSizeCorrectionKeepsWholeChunks(t *testing.T) {
	_, tor, _ := hostilePeer(t, false)
	qt.Assert(t, qt.IsNil(tor.setMetadataSize(5<<14)))
	for i := range 3 {
		tor.saveMetadataPiece(i, bytes.Repeat([]byte{byte(i + 1)}, 1<<14))
	}
	qt.Assert(t, qt.IsNil(tor.setMetadataSize(2<<14+100)))
	qt.Check(t, qt.HasLen(tor.metadataBytes, 2<<14+100))
	qt.Check(t, qt.DeepEquals(tor.metadataCompletedChunks, []bool{true, true, false}))
	qt.Check(t, qt.Equals(tor.metadataBytes[1<<14], byte(2)))
}

func TestHolepunchFromPeerThatNeverAdvertisedIt(t *testing.T) {
	cl, _, pc := hostilePeer(t, true)
	msg := utHolepunch.Msg{MsgType: utHolepunch.Rendezvous, AddrPort: netip.MustParseAddrPort("192.0.2.1:6881")}
	payload, err := msg.MarshalBinary()
	qt.Assert(t, qt.IsNil(err))
	cl.lock()
	defer cl.unlock()
	pc.LocalLtepProtocolMap = &cl.defaultLocalLtepProtocolMap
	for i, name := range pc.LocalLtepProtocolMap.Index {
		if name == utHolepunch.ExtensionName {
			qt.Check(t, qt.IsNotNil(pc.onReadExtendedMsg(pp.ExtensionNumber(i)+1, payload)))
			return
		}
	}
	t.Skip("ut_holepunch not in the local extension map")
}

func TestChunkMustBeTheOneAskedFor(t *testing.T) {
	_, tor, _ := hostilePeer(t, true)
	tor.cl.lock()
	defer tor.cl.unlock()
	last := tor.pieceLength(0)
	good := Request{Index: 0, ChunkSpec: ChunkSpec{Begin: 0, Length: 2}}
	qt.Check(t, qt.IsNil(tor.checkValidReceiveChunk(good)))
	for _, r := range []Request{
		{Index: 0, ChunkSpec: ChunkSpec{Begin: 1, Length: 2}},
		{Index: 0, ChunkSpec: ChunkSpec{Begin: 0, Length: 16384}},
		{Index: 0, ChunkSpec: ChunkSpec{Begin: last - 1, Length: 2}},
	} {
		qt.Check(t, qt.IsNotNil(tor.checkValidReceiveChunk(r)), qt.Commentf("%+v", r))
	}
}

func TestHaveAllThenHaveNoneLeavesAvailabilityBalanced(t *testing.T) {
	cl, tor, pc := hostilePeer(t, true)
	cl.lock()
	defer cl.unlock()
	pc.onPeerHasAllPiecesNoTriggers()
	qt.Check(t, qt.HasLen(tor.connsWithAllPieces, 1))
	qt.Check(t, qt.IsNil(pc.peerSentHaveNone()))
	qt.Check(t, qt.HasLen(tor.connsWithAllPieces, 0))
	qt.Check(t, qt.Equals(tor.piece(0).availability(), 0))
}
