package transcode

import (
	"strings"
	"testing"
)

func TestSafeFontName(t *testing.T) {
	for _, c := range []struct {
		stored string
		want   string
	}{
		{"Arial.ttf", "Arial.ttf"},
		{`C:\fonts\Arial.ttf`, "Arial.ttf"},
		{"../../evil.otf", "evil.otf"},
		{"???", "font3.ttf"},
		{"..", "font3.ttf"},
		{"", "font3.ttf"},
		{strings.Repeat("a", 240) + ".otf", "font3.otf"},
		{strings.Repeat("a", 240) + ".verylongext", "font3.ttf"},
	} {
		if got := safeFontName(c.stored, 3); got != c.want {
			t.Errorf("%q = %q, want %q", c.stored, got, c.want)
		}
	}
}
