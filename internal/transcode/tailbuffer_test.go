package transcode

import (
	"strings"
	"testing"
)

func TestTailBufferKeepsTheEnd(t *testing.T) {
	var b tailBuffer
	for range 5000 {
		b.Write([]byte(strings.Repeat("x", 99) + "\n"))
	}
	b.Write([]byte("the last line"))
	got := b.String()
	if len(got) > tailKeep || !strings.HasSuffix(got, "the last line") {
		t.Fatalf("kept %d bytes ending %q", len(got), got[max(0, len(got)-20):])
	}
	var big tailBuffer
	big.Write([]byte(strings.Repeat("y", 2*tailKeep) + "end"))
	if s := big.String(); len(s) != tailKeep || !strings.HasSuffix(s, "end") {
		t.Fatalf("one big write kept %d bytes", len(s))
	}
}
