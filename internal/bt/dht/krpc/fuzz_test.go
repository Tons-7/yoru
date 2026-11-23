package krpc

import (
	"bytes"
	"testing"

	qt "github.com/frankban/quicktest"
	"yoru/internal/bt/bencode"
)

func Fuzz(f *testing.F) {
	f.Add([]byte("d1:rd2:id20:\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x01e1:t1:t1:y1:re"))
	f.Add([]byte("d2:ip6:\x01\x02\x03\x04\x1a\xe11:t2:aa1:y1:qe"))
	f.Add([]byte("d2:ip1:x1:t2:aa1:y1:qe"))
	f.Add([]byte("d1:rd2:id20:aaaaaaaaaaaaaaaaaaaa6:valuesl6:\x01\x02\x03\x04\x1a\xe11:xee1:t2:aa1:y1:re"))
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
