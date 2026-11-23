package peer_protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessageUnmarshalBinaryRoundTrip(t *testing.T) {
	for _, m := range []Message{
		{Type: Request, Index: 1, Begin: 2, Length: 16384},
		{Type: Piece, Index: 3, Begin: 0, Piece: []byte{1, 2, 3}},
		{Type: Have, Index: 9},
		{Type: Extended, ExtendedID: 2, ExtendedPayload: []byte("d1:ai1ee")},
		{Keepalive: true},
	} {
		b, err := m.MarshalBinary()
		require.NoError(t, err)
		var got Message
		require.NoError(t, got.UnmarshalBinary(b), "%v", m)
		assert.Equal(t, m, got)
		assert.Error(t, new(Message).UnmarshalBinary(append(b, 0)), "trailing byte after %v", m)
	}
}
