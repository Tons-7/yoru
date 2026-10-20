package dht

import (
	"net"
	"strconv"
	"testing"

	"github.com/anacrolix/log"
	"github.com/stretchr/testify/require"
)

// One UDP packet with a query and no arguments used to take the whole process down.
func TestQueriesWithoutArgumentsAreRefused(t *testing.T) {
	s := newServer(t, log.Default)
	conn, err := net.Dial("udp", s.Addr().String())
	require.NoError(t, err)
	defer conn.Close()
	for _, q := range []string{"announce_peer", "put", "get", "get_peers", "find_node", "no_such_query"} {
		_, err := conn.Write([]byte("d1:q" + strconv.Itoa(len(q)) + ":" + q + "1:t2:aa1:y1:qe"))
		require.NoError(t, err)
		reply := make([]byte, 512)
		n, err := conn.Read(reply)
		require.NoError(t, err, q)
		require.Contains(t, string(reply[:n]), "1:y1:e", q)
	}
	// Still serving.
	other := newServer(t, log.Default)
	require.NoError(t, other.Ping(s.Addr().(*net.UDPAddr)).ToError())
}
