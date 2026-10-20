package transcode

import (
	"os"
	"path/filepath"
	"testing"
)

// Zeros are a font still downloading, worth waiting for; an empty or foreign file never becomes one.
func TestFontHeader(t *testing.T) {
	dir := t.TempDir()
	for name, c := range map[string]struct {
		body []byte
		want fontState
	}{
		"whole.ttf":    {[]byte("\x00\x01\x00\x00rest"), fontWhole},
		"otf.otf":      {[]byte("OTTOrest"), fontWhole},
		"arriving.ttf": {make([]byte, 64), fontArriving},
		"empty.ttf":    {nil, fontBroken},
		"html.ttf":     {[]byte("<html>"), fontBroken},
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
