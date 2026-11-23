package library

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"yoru/internal/indexer"
	"yoru/internal/parse"
	"yoru/internal/score"
)

var broadcastForm = []struct {
	title    string
	accepted bool
}{
	{"[AnoZu] Bleach S17E42 1080p CR WEB-DL AAC 2.0 H.264", true},
	{"Bleach S17E42 Thousand-Year Blood War SON OF DARKNESS 1080p DSNP WEB-DL AAC2.0 H 264-NTb", true},
	{"[Breeze] Bleach Thousand-Year Blood War S17E42 [1080p AV1 Dual Audio] (weekly)", true},
	{"[DKB] Bleach - Sennen Kessen-hen - 42 [1080p][HEVC x265 10bit][Multi-Subs][234F1874].mkv", true},
	{"[AnoZu] Bleach S17E43 1080p CR WEB-DL AAC 2.0 H.264", false},
}

func TestUnnumberedPackConfirmsNothing(t *testing.T) {
	st := bleachStore(t)
	ctx := context.Background()
	f := NewFinder(st, fixedIndexer{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	titles, _ := st.SearchTitles(ctx, tybwCalamity)
	english, _ := st.EnglishTitle(ctx, tybwCalamity)
	req := f.numbering(ctx, Request{AnimeID: tybwCalamity, Episode: 2}, titles, english)

	pack := parse.Parse("[ASW] Bleach - Sennen Kessen-hen [1080p HEVC x265 10Bit][AAC] (Batch)")
	if confirms(pack, req) {
		t.Error("a pack stating no range confirmed episode 2")
	}
	ranged := parse.Parse("[ASW] Bleach - Sennen Kessen-hen - Kashin-tan (01-13) [1080p] [Batch]")
	if !confirms(ranged, req) {
		t.Error("a pack whose stated range holds episode 2 should confirm it")
	}
}

func TestBroadcastSeasonReleasesReachALaterCour(t *testing.T) {
	st := bleachStore(t)
	ctx := context.Background()

	var results []indexer.Torrent
	for i, r := range broadcastForm {
		results = append(results, release(strings.Repeat("b", 39)+string(rune('0'+i)), r.title, 100))
	}
	f := NewFinder(st, fixedIndexer{results: results}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	found, err := f.Find(ctx, Request{
		AnimeID: tybwCalamity, Episode: 2, Prefs: score.DefaultPreferences(),
	})
	if err != nil {
		t.Fatal(err)
	}

	kept := map[string]bool{}
	for _, r := range found.Results {
		kept[r.Torrent.Title] = true
	}
	for _, want := range broadcastForm {
		if kept[want.title] != want.accepted {
			t.Errorf("accepted=%v, want %v: %s", kept[want.title], want.accepted, want.title)
		}
	}
	if found.Best == nil {
		t.Error("nothing pickable for episode 2 of the current cour")
	}
}
