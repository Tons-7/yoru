package dht

import (
	"crypto/rand"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnnounceNoStartingNodes(t *testing.T) {
	s, err := NewServer(&ServerConfig{
		Conn:       mustListen(":0"),
		NoSecurity: true,
	})
	require.NoError(t, err)
	defer s.Close()
	var ih [20]byte
	copy(ih[:], "blah")
	_, err = s.Announce(ih, 0, true)
	require.EqualError(t, err, "no initial nodes")
}

func randomInfohash() (ih [20]byte) {
	rand.Read(ih[:])
	return
}

func TestAnnounceStopsNoPending(t *testing.T) {
	s, err := NewServer(&ServerConfig{
		Conn: mustListen(":0"),
		StartingNodes: func() ([]Addr, error) {
			return []Addr{NewAddr(&net.TCPAddr{})}, nil
		},
	})
	require.NoError(t, err)
	defer s.Close()
	a, err := s.Announce(randomInfohash(), 0, true)
	require.NoError(t, err)
	defer a.Close()
	<-a.Peers
}
