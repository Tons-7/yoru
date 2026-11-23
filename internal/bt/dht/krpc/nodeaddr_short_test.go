package krpc

import (
	"strings"
	"testing"

	"yoru/internal/bt/bencode"
)

func TestUnmarshalNodeAddrShort(t *testing.T) {
	for _, b := range [][]byte{nil, {1}, {1, 2, 3, 4, 5}} {
		var a NodeAddr
		if err := a.UnmarshalBinary(b); err == nil {
			t.Errorf("%d bytes read as an address", len(b))
		}
	}
	id := strings.Repeat("a", 20)
	for _, packet := range []string{
		"d2:ip1:x1:t2:aa1:y1:qe",
		"d1:rd2:id20:" + id + "6:valuesl1:xee1:t2:aa1:y1:re",
	} {
		var m Msg
		if err := bencode.Unmarshal([]byte(packet), &m); err == nil {
			t.Errorf("a packet with a 1-byte address decoded: %q", packet)
		}
	}
}
