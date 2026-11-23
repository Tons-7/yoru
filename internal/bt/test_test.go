package bt

import (
	"testing"

	"github.com/go-quicktest/qt"

	"yoru/internal/bt/metainfo"
)

func newTestingClient(t testing.TB) *Client {
	cl, err := NewClient(TestingConfig(t))
	qt.Assert(t, qt.IsNil(err))
	t.Cleanup(func() {
		cl.Close()
	})
	return cl
}

var testingTorrentInfoHash = metainfo.Hash{1}

var testingAddTorrentOpts = AddTorrentOpts{
	InfoHash:                 testingTorrentInfoHash,
	DisableInitialPieceCheck: true,
}

func (cl *Client) newTorrentForTesting() *Torrent {
	return cl.newTorrentOpt(testingAddTorrentOpts)
}
