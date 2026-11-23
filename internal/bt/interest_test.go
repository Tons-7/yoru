package bt

import (
	"testing"

	"yoru/internal/bt/internal/testutil"
	"yoru/internal/bt/storage"
)

func TestInterestIsStickyUntilPeerHasNothingWeLack(t *testing.T) {
	mi := testutil.GreetingMetaInfo()
	conn := func(withInfo bool) (*PeerConn, *Torrent) {
		cl := newTestingClient(t)
		cs := storage.NewFile(t.TempDir())
		t.Cleanup(func() { cs.Close() })
		tt := cl.newTorrent(mi.HashInfoBytes(), cs)
		cl.lock()
		defer cl.unlock()
		tt.initialPieceCheckDisabled = true
		if withInfo {
			if err := tt.setInfoBytesLocked(mi.InfoBytes); err != nil {
				t.Fatal(err)
			}
		}
		pc := cl.newConnection(nil, newConnectionOpts{network: "test"})
		pc.setTorrent(tt)
		return pc, tt
	}

	pc, _ := conn(false)
	if pc.allowSendNotInterested() {
		t.Error("not interested was allowed before the peer's pieces are known")
	}

	pc, tt := conn(true)
	pc.peerSentHaveAll = true
	if pc.allowSendNotInterested() {
		t.Error("not interested was allowed to a seeder that has pieces we lack")
	}

	pc, tt = conn(true)
	pc._peerPieces.Add(0)
	tt._completedPieces.Add(0)
	if !pc.allowSendNotInterested() {
		t.Error("not interested was refused though the peer has nothing we lack")
	}

	pc, tt = conn(true)
	pc.peerSentHaveAll = true
	for i := range tt.numPieces() {
		tt._completedPieces.Add(uint32(i))
	}
	if !pc.allowSendNotInterested() {
		t.Error("not interested was refused though we have every piece")
	}
}
