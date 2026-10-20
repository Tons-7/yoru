//go:build go1.18

package krpc

import (
	"bytes"
	"testing"

	qt "github.com/frankban/quicktest"
	"yoru/internal/bt/bencode"
)

// A Msg that unmarshals marshals back without error; not to the same bytes, as unknown fields go, but
// one that does is an interesting case.
func Fuzz(f *testing.F) {
	f.Add([]byte("d1:rd2:id20:\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x01e1:t1:t1:y1:re"))
	f.Fuzz(func(t *testing.T, in []byte) {
		c := qt.New(t)
		var m Msg
		err := bencode.Unmarshal(in, &m)
		if err != nil {
			t.Skip()
		}
		out, err := bencode.Marshal(m)
		c.Assert(err, qt.IsNil)
		if !bytes.Equal(in, out) {
			t.Skip()
		}
	})
}
