package library

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"yoru/internal/indexer"
	"yoru/internal/parse"
	"yoru/internal/score"
)

// A tier of no listed resolution ("BD") stays, after the ones the limit allows and before those above it.
func TestUpToKeepsTiersOfNoResolution(t *testing.T) {
	ladder := []score.Tier{score.ParseTier("1080p:BD"), score.ParseTier("BD"), score.ParseTier("720p")}
	got := upTo("720p", ladder)
	at := func(tier string) int {
		return slices.Index(got, score.ParseTier(tier))
	}
	if at("BD") < 0 || at("1080p:BD") < 0 || at("720p") < 0 {
		t.Fatalf("a tier went missing: %v", got)
	}
	if !(at("720p") < at("480p") && at("480p") < at("BD") && at("BD") < at("1080p:BD") && at("1080p:BD") < at("2160p")) {
		t.Errorf("order: %v", got)
	}
}

// "Up to 720p" picks the best release at or under it, and a bigger one only when nothing fits.
func TestPreferredQualityIsAnUpperLimit(t *testing.T) {
	st := prefetchStore(t)
	ctx := context.Background()
	pick := func(resolutions ...string) string {
		t.Helper()
		p, err := st.Prefs(ctx, 0)
		if err != nil {
			t.Fatal(err)
		}
		var cands []score.Candidate
		for i, res := range resolutions {
			title := fmt.Sprintf("[Group] Show - 01 [%s].mkv", res)
			cands = append(cands, score.Candidate{
				Torrent:   indexer.Torrent{Title: title, InfoHash: fmt.Sprint(i), Seeders: 50, SeedersKnown: true, Size: 1 << 30},
				Release:   parse.Parse(title),
				Confirmed: true,
			})
		}
		best, ok := score.Best(cands, Preferences(p))
		if !ok {
			t.Fatalf("nothing picked from %v", resolutions)
		}
		return best.Release.Resolution
	}
	set := func(v string) {
		t.Helper()
		if err := st.SetSetting(ctx, "quality.prefer", v); err != nil {
			t.Fatal(err)
		}
	}

	if got := pick("480p", "1080p", "720p"); got != "1080p" {
		t.Errorf("best available picked %s, want 1080p", got)
	}
	set("720p")
	for _, c := range []struct {
		offered []string
		want    string
	}{
		{[]string{"480p", "1080p", "720p"}, "720p"},
		{[]string{"1080p", "480p"}, "480p"},
		{[]string{"1080p", "2160p"}, "1080p"},
	} {
		if got := pick(c.offered...); got != c.want {
			t.Errorf("up to 720p, from %v: picked %s, want %s", c.offered, got, c.want)
		}
	}
	set("480p")
	if got := pick("1080p", "720p"); got != "720p" {
		t.Errorf("up to 480p with nothing that small: picked %s, want the nearest above, 720p", got)
	}
}
