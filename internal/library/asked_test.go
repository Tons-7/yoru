package library

import (
	"context"
	"testing"

	"yoru/internal/indexer"
	"yoru/internal/score"
)

// Words typed in the picker are searched first, and a pasted magnet is listed whatever its name says; both stay
// for the play that picks from them, which looks the release up again by hash.
func TestAskedWordsAndPastedMagnetReachTheNextFind(t *testing.T) {
	st := monsterStore(t)
	f := NewFinder(st, fixedIndexer{results: []indexer.Torrent{
		release("3333333333333333333333333333333333333333", "[sam] Monster (2004) - 03 (DVD 572p)", 48),
	}}, discard())
	ctx := context.Background()
	pastedHash := "4444444444444444444444444444444444444444"
	f.Ask(19, 3, "kaibutsu dvd", nil)
	f.Ask(19, 3, "", &indexer.Torrent{Title: "Some group's own name, part five", InfoHash: pastedHash, Indexer: Pasted})

	for range 2 { // the picker's search, then the play that picks from it
		got, err := f.Find(ctx, Request{AnimeID: 19, Episode: 3, Season: 1, Prefs: score.DefaultPreferences()})
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Queries) == 0 || got.Queries[0] != "kaibutsu dvd" {
			t.Fatalf("queries = %v, want the typed words first", got.Queries)
		}
		var pasted *score.Result
		for i := range got.Results {
			if got.Results[i].Torrent.InfoHash == pastedHash {
				pasted = &got.Results[i]
			}
		}
		if pasted == nil {
			t.Fatal("the pasted magnet is not among the results")
		}
		if pasted.WrongShow {
			t.Error("a pasted release was judged another show by its name")
		}
		// Only a person's pick plays it: play, prefetch and the watcher choose among AutoPick ones.
		if pasted.AutoPick || (got.Best != nil && got.Best.Torrent.InfoHash == pastedHash) {
			t.Error("a pasted release could be chosen automatically")
		}
	}

	// Pasting is for when the sites fail: the magnet is offered with every site down, or none set up.
	for name, idx := range map[string]indexer.Source{"sites down": downIndexer{}, "no sites": nil} {
		lone := NewFinder(st, idx, discard())
		lone.Ask(19, 3, "", &indexer.Torrent{Title: "Pasted", InfoHash: pastedHash, Indexer: Pasted})
		got, err := lone.Find(ctx, Request{AnimeID: 19, Episode: 3, Season: 1, Prefs: score.DefaultPreferences()})
		if err != nil || len(got.Results) != 1 || got.Results[0].Torrent.InfoHash != pastedHash {
			t.Errorf("%s: %d results, %v; want the pasted magnet alone", name, len(got.Results), err)
		}
	}

	// Another episode keeps its own search.
	other, err := f.Find(ctx, Request{AnimeID: 19, Episode: 4, Season: 1, Prefs: score.DefaultPreferences()})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range other.Results {
		if r.Torrent.InfoHash == pastedHash {
			t.Fatal("episode 3's pasted magnet turned up for episode 4")
		}
	}
}
