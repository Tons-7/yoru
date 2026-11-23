package bt

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"yoru/internal/bt/dht"
	"yoru/internal/bt/dht/krpc"
)

func localDhtServer(t *testing.T) *dht.Server {
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	require.NoError(t, err)
	cfg := dht.NewDefaultServerConfig()
	cfg.Conn = pc
	cfg.StartingNodes = func() ([]dht.Addr, error) { return nil, nil }
	s, err := dht.NewServer(cfg)
	require.NoError(t, err)
	t.Cleanup(s.Close)
	return s
}

func TestDhtNodesSurviveARestart(t *testing.T) {
	file := filepath.Join(t.TempDir(), "dht-nodes")
	first := localDhtServer(t)
	for i := 1; i <= 5; i++ {
		var id krpc.ID
		id[0], id[19] = byte(i), byte(i)
		require.NoError(t, first.AddNode(krpc.NodeInfo{ID: id, Addr: krpc.NodeAddr{IP: net.IPv4(8, 8, 4, byte(i)).To4(), Port: 6881}}))
	}
	saveDhtNodes(file, []*dht.Server{first})
	_, err := os.Stat(file + ".tmp")
	require.True(t, os.IsNotExist(err))

	second := localDhtServer(t)
	loadDhtNodes(file, []*dht.Server{second})
	require.Equal(t, first.NumNodes(), second.NumNodes())
	require.Equal(t, 5, second.NumNodes())

	saveDhtNodes(file, []*dht.Server{localDhtServer(t)})
	third := localDhtServer(t)
	loadDhtNodes(file, []*dht.Server{third})
	require.Equal(t, 5, third.NumNodes(), "an empty table must not overwrite the saved one")
}
