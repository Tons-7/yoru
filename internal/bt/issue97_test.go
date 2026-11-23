package bt

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"yoru/internal/bt/internal/testutil"
	"yoru/internal/bt/storage"
)

func TestVerifyDataAfterDrop(t *testing.T) {
	cl := newTestingClient(t)
	cs := storage.NewFile(t.TempDir())
	defer cs.Close()
	mi := testutil.GreetingMetaInfo()
	tt := cl.newTorrent(mi.HashInfoBytes(), cs)
	require.NoError(t, tt.SetInfoBytes(mi.InfoBytes))
	tt.Drop()
	require.ErrorIs(t, tt.Piece(0).VerifyDataContext(t.Context()), errTorrentClosed)
	require.ErrorIs(t, tt.VerifyDataContext(t.Context()), errTorrentClosed)
	locked := make(chan struct{})
	go func() {
		cl.lock()
		cl.unlock()
		close(locked)
	}()
	select {
	case <-locked:
	case <-time.After(5 * time.Second):
		t.Fatal("the client lock was left held")
	}
}

func TestForeignMetadataIsRefused(t *testing.T) {
	cl := newTestingClient(t)
	cs := storage.NewFile(t.TempDir())
	defer cs.Close()
	mi := testutil.GreetingMetaInfo()
	tt := cl.newTorrent(mi.HashInfoBytes(), cs)
	cl.lock()
	defer cl.unlock()
	require.NoError(t, tt.setMetadataSize(len(mi.InfoBytes)))
	foreign := append([]byte(nil), mi.InfoBytes...)
	foreign[len(foreign)-2] ^= 1
	require.Error(t, tt.setInfoBytesLocked(foreign))
	require.Nil(t, tt.info)
	require.NotNil(t, tt.metadataCompletedChunks)
	require.NoError(t, tt.setInfoBytesLocked(mi.InfoBytes))
	require.NotNil(t, tt.info)
}

func TestHashPieceAfterStorageClosed(t *testing.T) {
	cl := newTestingClient(t)
	td := t.TempDir()
	cs := storage.NewFile(td)
	defer cs.Close()
	mi := testutil.GreetingMetaInfo()
	tt := cl.newTorrent(mi.HashInfoBytes(), cs)
	require.NoError(t, tt.SetInfoBytes(mi.InfoBytes))
	go tt.piece(0).VerifyDataContext(t.Context())
	require.NoError(t, tt.storage.Close())
}
