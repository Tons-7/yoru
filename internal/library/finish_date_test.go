package library

import (
	"context"
	"testing"
	"time"

	"yoru/internal/store"
)

// Finishing the last episode completes the entry, and AniList is sent today as its finish date: a slip here
// writes a wrong or empty date to the user's public profile for every show they finish.
func TestPushSendsTheFinishDate(t *testing.T) {
	up := &aniServer{list: `{"data":{"MediaListCollection":{"lists":[]}}}`}
	sync, st := newAniSync(t, up)
	ctx := context.Background()
	episodes := 12
	if _, err := st.ImportList(ctx, []store.Anime{{ID: 100, Romaji: "Frieren", Genres: "[]", Episodes: &episodes}},
		nil, store.ImportMerge); err != nil {
		t.Fatal(err)
	}
	if _, err := st.MarkWatched(ctx, 100, 12); err != nil {
		t.Fatal(err)
	}
	if err := sync.PushOne(ctx, 100); err != nil {
		t.Fatal(err)
	}

	up.mu.Lock()
	saves := append([]map[string]any{}, up.saves...)
	up.mu.Unlock()
	if len(saves) == 0 {
		t.Fatal("nothing was pushed")
	}
	sent := saves[len(saves)-1]
	if sent["status"] != "COMPLETED" {
		t.Fatalf("status = %v, want COMPLETED", sent["status"])
	}
	date, _ := sent["completedAt"].(map[string]any)
	now := time.Now()
	if date["year"] != float64(now.Year()) || date["month"] != float64(now.Month()) || date["day"] != float64(now.Day()) {
		t.Fatalf("completedAt = %v, want %s", date, now.Format("2006-01-02"))
	}
}

func TestFuzzyDate(t *testing.T) {
	for _, c := range []struct {
		in               string
		year, month, day int // 0: no date
	}{
		{"2026-10-19", 2026, 10, 19},
		{"", 0, 0, 0},
		{"2026-10", 0, 0, 0},
		{"0000-01-01", 0, 0, 0},
		{"2026-1x-01", 0, 0, 0},
	} {
		got := fuzzyDate(c.in)
		if c.year == 0 {
			if got != nil {
				t.Errorf("%q: %+v, want no date", c.in, got)
			}
			continue
		}
		if got == nil || *got.Year != c.year || *got.Month != c.month || *got.Day != c.day {
			t.Errorf("%q: %+v, want %d-%d-%d", c.in, got, c.year, c.month, c.day)
		}
	}
}
