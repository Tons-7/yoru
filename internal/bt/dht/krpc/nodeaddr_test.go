package krpc

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"yoru/internal/bt/bencode"
)

var (
	IPv4    = net.IPv4
	ParseIP = net.ParseIP
)

func TestUnmarshalNodeAddr(t *testing.T) {
	var na NodeAddr
	require.NoError(t, na.UnmarshalBinary([]byte("\x01\x02\x03\x04\x05\x06")))
	assert.EqualValues(t, "1.2.3.4", na.IP.String())
}

func TestUnmarshalNodeAddrWrongLength(t *testing.T) {
	for _, b := range [][]byte{nil, {1}, {1, 2}, {1, 2, 3, 4, 5}, make([]byte, 19)} {
		var na NodeAddr
		assert.Error(t, na.UnmarshalBinary(b), "%d bytes", len(b))
	}
	var na NodeAddr
	require.NoError(t, na.UnmarshalBinary(append(net.ParseIP("2001:db8::1").To16(), 0, 80)))
	assert.EqualValues(t, 80, na.Port)
}

func TestShortNodeAddrInMessageIsAnError(t *testing.T) {
	for _, packet := range []string{
		"d2:ip1:x1:t2:aa1:y1:qe",
		"d2:ip0:1:t2:aa1:y1:qe",
		"d1:rd2:id20:aaaaaaaaaaaaaaaaaaaa6:valuesl1:xee1:t2:aa1:y1:re",
	} {
		var m Msg
		assert.NotPanics(t, func() { assert.Error(t, bencode.Unmarshal([]byte(packet), &m), packet) })
	}
}

var naEqualTests = []struct {
	a, b NodeAddr
	out  bool
}{
	{NodeAddr{IPv4(172, 16, 1, 1), 11}, NodeAddr{IPv4(172, 16, 1, 1), 11}, true},
	{NodeAddr{IPv4(172, 16, 1, 1), 11}, NodeAddr{IPv4(172, 16, 1, 1), 22}, false},
	{NodeAddr{IPv4(172, 16, 1, 1), 11}, NodeAddr{IPv4(192, 168, 0, 3), 11}, false},
	{NodeAddr{IPv4(172, 16, 1, 1), 11}, NodeAddr{IPv4(192, 168, 0, 3), 22}, false},
	{NodeAddr{ParseIP("2001:db8:1:2::1"), 11}, NodeAddr{ParseIP("2001:db8:1:2::1"), 11}, true},
	{NodeAddr{ParseIP("2001:db8:1:2::1"), 11}, NodeAddr{ParseIP("2001:db8:1:2::1"), 22}, false},
	{NodeAddr{ParseIP("2001:db8:1:2::1"), 11}, NodeAddr{ParseIP("fe80::420b"), 11}, false},
	{NodeAddr{ParseIP("2001:db8:1:2::1"), 11}, NodeAddr{ParseIP("fe80::420b"), 22}, false},
}

func TestNodeAddrEqual(t *testing.T) {
	for _, tc := range naEqualTests {
		out := tc.a.Equal(tc.b)
		if out != tc.out {
			t.Errorf("NodeAddr(%v).Equal(%v) = %v, want %v", tc.a, tc.b, out, tc.out)
		}
	}
}
