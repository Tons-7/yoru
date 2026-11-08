package library

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"yoru/internal/indexer"
	"yoru/internal/metadata"
	"yoru/internal/store"
	"yoru/internal/torrent"
)

func crownHandlerStore(t *testing.T) *store.Store {
	t.Helper()
	st := openStore(t)
	ctx := context.Background()
	ch4 := entryOf(4, "MOVIE", "Princess Principal: Crown Handler - Chapter 4", "", "2025-01-01", 1, 60)
	ch4.Synonyms = `["Princess Principal Movie 4"]`
	anime := []store.Anime{
		entryOf(1, "TV", "Princess Principal", "", "2017-07-01", 12, 24),
		entryOf(2, "MOVIE", "Princess Principal: Crown Handler - Chapter 1", "", "2021-01-01", 1, 50),
		entryOf(3, "MOVIE", "Princess Principal: Crown Handler - Chapter 2", "", "2021-09-01", 1, 56),
		ch4,
		entryOf(5, "OVA", "Princess Principal: Crown Handler - Chapter 1: BUSY EASY MONEY", "", "2021-01-01", 1, 6),
	}
	if _, err := st.ImportList(ctx, anime, nil, store.ImportMerge); err != nil {
		t.Fatal(err)
	}
	rels := []store.Relation{
		{AnimeID: 1, RelatedID: 2, Kind: "SEQUEL"}, {AnimeID: 2, RelatedID: 3, Kind: "SEQUEL"},
		{AnimeID: 3, RelatedID: 4, Kind: "SEQUEL"}, {AnimeID: 2, RelatedID: 5, Kind: "SIDE_STORY"},
	}
	if err := st.SaveRelations(ctx, nil, rels); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RebuildFranchises(ctx); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestFilmsSharingALongerNameThanTheirShowAreASeries(t *testing.T) {
	st := crownHandlerStore(t)
	got, _ := findFilm(t, st, 4, []indexer.Torrent{
		release("techmod", "[Techmod] Princess Principal Crown Handler - Movie 4 [1080p]", 22),
		release("flac", "[Anime-FLAC] Princess Principal - Crown Handler Movies (Chapter 1-4) (BD 1080p) (Batch) (Ongoing)", 5),
		release("sp02", "[SubsPlease] Princess Principal - Crown Handler - 02 (1080p) [41FDC70B].mkv", 8),
		release("tv04", "[SubsPlease] Princess Principal - 04 (1080p) [ABCD1234].mkv", 30),
		release("tvpack", "[Group] Princess Principal (01-12) [BD 1080p] [Batch]", 40),
	})
	if r := byHash(t, got, "techmod"); !r.AutoPick {
		t.Errorf("the film's own release was blocked: %q", r.Blocked)
	}
	if r := byHash(t, got, "flac"); r.WrongFilm || !r.Release.Batch {
		t.Errorf("a batch of chapters 1-4 was refused for chapter 4: %q", r.Blocked)
	}
	if r := byHash(t, got, "sp02"); !r.WrongFilm {
		t.Error("chapter 2 passed as chapter 4")
	}
	for _, h := range []string{"tv04", "tvpack"} {
		if r := byHash(t, got, h); r.AutoPick {
			t.Errorf("the TV show's %q passed as the film", r.Torrent.Title)
		}
	}
	if got.Best == nil || got.Best.Torrent.InfoHash != "techmod" {
		t.Errorf("best = %+v, want the film's own release", got.Best)
	}
}

func TestTheSeriesNameIsSearchedEvenWhenItIsOneOfTheTitles(t *testing.T) {
	titles := []string{
		"Princess Principal: Crown Handler 1 - Busy Easy Money",
		"Princess Principal: Crown Handler - Chapter 1: BUSY EASY MONEY",
		"Princess Principal OVA",
		"Princess Principal: Crown Handler",
		"Princess Principal: Crown Handler 1 OVA - Busy Easy Money",
		"Princess Principal: Crown Handler Movie 1 - Busy Easy Money",
		"Princess Principal: Crown Handler Chapter 1 - Busy Easy Money",
	}
	terms := searchTerms(titles, "", 1, 0, 0, nil, false, "Princess Principal Crown Handler")
	if !slices.Contains(terms, "Princess Principal Crown Handler") {
		t.Errorf("terms %q; the series name finds its batches and must be searched", terms)
	}
	seen := map[string]bool{}
	for _, q := range terms {
		if seen[strings.ToLower(q)] {
			t.Errorf("%q searched twice", q)
		}
		seen[strings.ToLower(q)] = true
	}
}

func TestAMissingFileListIsTriedAgainLaterInWords(t *testing.T) {
	err := fmt.Errorf("tried 1 release(s) for episode 1 without one that would download: %w", torrent.ErrNoFileList)
	if !swarmFailure(err) {
		t.Error("a file list that never came failed at once instead of waiting for the swarm")
	}
	if got := describe(err); got.Error() != torrent.ErrNoFileList.Error() {
		t.Errorf("described as %q", got)
	}
}

func TestAHeldReleaseOfAnotherFilmIsNotResumed(t *testing.T) {
	heldFor := func(name string, manual bool) bool {
		st := crownHandlerStore(t)
		ctx := context.Background()
		if err := st.RecordTorrent(ctx, store.TorrentRecord{InfoHash: "1111111111111111111111111111111111111111",
			Name: name, AnimeID: 5, EpKey: "1", FilePath: name, TotalSize: 100, Manual: manual}); err != nil {
			t.Fatal(err)
		}
		_, ok := NewPrefetcher(st, NewFinder(st, fixedIndexer{}, discard()), nil, discard()).held(ctx, 5, 1)
		return ok
	}
	film := "[ASW] Princess Principal - Crown Handler - 01 [1080p HEVC].mkv"
	if heldFor(film, false) {
		t.Error("the chapter 1 film held for its side story was resumed")
	}
	if !heldFor(film, true) {
		t.Error("a hand pick was second-guessed")
	}
	if !heldFor("[Anime Chap] Princess Principal - Crown Handler 1: OVA 1 (Easy Busy Money) [BD 1080p]", false) {
		t.Error("the side story's own release was dropped")
	}
	if !heldFor("[Anime Chap] Princess Principal - Crown Handler 1 OVA 1 [BD 1080p].mkv", false) {
		t.Error("the side story's release was dropped for the name inside its torrent")
	}
}

func TestASeaDexListedHeldReleaseStandsWhateverItsName(t *testing.T) {
	st := monsterStore(t)
	ctx := context.Background()
	const hash = "5555555555555555555555555555555555555555"
	if _, err := st.SaveSeaDex(ctx, []metadata.SeaDexRelease{{AniListID: 19, InfoHash: hash, ReleaseGroup: "Kaibutsu", Tracker: "Nyaa", IsBest: true}}); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordTorrent(ctx, store.TorrentRecord{InfoHash: hash, Name: "[Kaibutsu] Urasawa Collection 01-74 (BD 1080p)",
		AnimeID: 19, EpKey: "3", FilePath: "Urasawa 03.mkv", TotalSize: 100}); err != nil {
		t.Fatal(err)
	}
	if _, ok := NewPrefetcher(st, NewFinder(st, fixedIndexer{}, discard()), nil, discard()).held(ctx, 19, 3); !ok {
		t.Error("SeaDex's own pick for the entry was dropped for its name, so every play searches again")
	}
}
