package transcode

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func sfnt(tag string, table []byte) []byte {
	out := make([]byte, 12+16)
	copy(out, tag)
	binary.BigEndian.PutUint16(out[4:], 1)
	copy(out[12:], "glyf")
	binary.BigEndian.PutUint32(out[20:], uint32(len(out)))
	binary.BigEndian.PutUint32(out[24:], uint32(len(table)))
	return append(out, table...)
}

func TestFontHeader(t *testing.T) {
	dir := t.TempDir()
	filled := make([]byte, 128)
	for i := range filled {
		filled[i] = byte(i + 1)
	}
	tail := append(make([]byte, 8000), filled...)
	for name, c := range map[string]struct {
		body []byte
		want fontState
	}{
		"whole.ttf":     {sfnt("\x00\x01\x00\x00", filled), fontWhole},
		"otf.otf":       {sfnt("OTTO", filled), fontWhole},
		"holes.ttf":     {sfnt("\x00\x01\x00\x00", make([]byte, 128)), fontArriving},
		"truncated.ttf": {sfnt("\x00\x01\x00\x00", filled)[:60], fontBroken},
		"woff.woff":     {append([]byte("wOFF"), tail...), fontWhole},
		"woff-holes":    {append([]byte("wOFF"), make([]byte, 8000)...), fontArriving},
		"arriving.ttf":  {make([]byte, 64), fontArriving},
		"empty.ttf":     {nil, fontBroken},
		"html.ttf":      {[]byte("<html>"), fontBroken},
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, c.body, 0o644); err != nil {
			t.Fatal(err)
		}
		if got := fontHeader(path); got != c.want {
			t.Errorf("%s: %d, want %d", name, got, c.want)
		}
	}
}
