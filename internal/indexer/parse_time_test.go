package indexer

import (
	"testing"
	"time"
)

func TestParseTimeReadsEveryFeedLayout(t *testing.T) {
	want := time.Date(2026, 8, 6, 17, 50, 0, 0, time.UTC)
	for _, s := range []string{
		"Thu, 06 Aug 2026 17:50:00 +0000",
		"Thu, 06 Aug 2026 17:50:00 UTC",
		"06 Aug 26 17:50 +0000",
		"06 Aug 26 17:50 UTC",
		"  Thu, 06 Aug 2026 17:50:00 -0000  ",
	} {
		if got := parseTime(s); !got.Equal(want) {
			t.Errorf("parseTime(%q) = %v, want %v", s, got, want)
		}
	}
	if got := parseTime("yesterday"); !got.IsZero() {
		t.Errorf("an unreadable date gave %v", got)
	}
}
