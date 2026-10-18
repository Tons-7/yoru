package library

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"testing"

	"yoru/internal/indexer"
	"yoru/internal/metadata"
	"yoru/internal/store"
)

// Jujutsu Kaisen as AniList has it: the film between the first and second seasons.
const jjk1, jjk0, jjk2, jjk3 = 113415, 131573, 145064, 172463

func jjkStore(t *testing.T) *store.Store {
	t.Helper()
	st := openStore(t)
	ctx := context.Background()
	anime := []store.Anime{
		entryOf(jjk1, "TV", "Jujutsu Kaisen", "JUJUTSU KAISEN", "2020-10-03", 24, 24),
		entryOf(jjk0, "MOVIE", "Jujutsu Kaisen 0", "JUJUTSU KAISEN 0", "2021-12-24", 1, 105),
		entryOf(jjk2, "TV", "Jujutsu Kaisen 2nd Season", "JUJUTSU KAISEN Season 2", "2023-07-06", 23, 24),
		entryOf(jjk3, "TV", "Jujutsu Kaisen: Shimetsu Kaiyuu - Zenpen", "JUJUTSU KAISEN Season 3: The Culling Game Part 1", "2026-01-08", 12, 24),
	}
	if _, err := st.ImportList(ctx, anime, nil, store.ImportMerge); err != nil {
		t.Fatal(err)
	}
	rels := []store.Relation{
		{AnimeID: jjk1, RelatedID: jjk0, Kind: "PREQUEL"},
		{AnimeID: jjk1, RelatedID: jjk2, Kind: "SEQUEL"},
		{AnimeID: jjk2, RelatedID: jjk3, Kind: "SEQUEL"},
	}
	if err := st.SaveRelations(ctx, nil, rels); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RebuildFranchises(ctx); err != nil {
		t.Fatal(err)
	}
	// TheTVDB files the film as special 2; catalogues fetched before that was dropped still say so.
	if _, err := st.SaveEpisodes(ctx, jjk0, []metadata.Episode{{Number: 1, TvdbNumber: 2}}); err != nil {
		t.Fatal(err)
	}
	return st
}

// What a search for the film can turn up: the show's episodes under the same name, and the film.
func jjkReleases() []indexer.Torrent {
	return []indexer.Torrent{
		sized("s3e02", "JUJUTSU.KAISEN.S03E02.One.More.Time.1080p.CR.WEB-DL.DUAL.AAC2.0.H.264-VARYG.mkv", 90, 1.4),
		sized("s1e01", "Jujutsu.Kaisen.S01E01.Ryomen.Sukuna.1080p.CR.WEB-DL.AAC2.0.H.264-VARYG.mkv", 80, 1.4),
		sized("sp01", "[SubsPlease] Jujutsu Kaisen - 01 (1080p) [8AE2A4A5].mkv", 70, 1.4),
		// The second season's first episode, counted on from the first.
		sized("sp25", "[SubsPlease] Jujutsu Kaisen - 25 (1080p) [9F2A4C11].mkv", 60, 1.4),
		sized("judas", "[Judas] Jujutsu Kaisen 0 (The Movie) [BD 1080p][HEVC x265 10bit][Dual-Audio][Multi-Subs]", 30, 4.2),
		sized("erai", "[Erai-raws] Jujutsu Kaisen 0 Movie [1080p][Multiple Subtitle]", 20, 3.1),
	}
}

func kept(c Candidates) map[string]bool {
	out := map[string]bool{}
	for _, r := range c.Results {
		out[r.Torrent.InfoHash] = true
	}
	return out
}

// A film named like its show is none of the show's episodes, whatever its stale TheTVDB number says.
func TestFilmIsNotItsShowsEpisode(t *testing.T) {
	st := jjkStore(t)
	got, _ := findFilm(t, st, jjk0, jjkReleases())

	have := kept(got)
	for _, hash := range []string{"s3e02", "s1e01", "sp01", "sp25"} {
		if have[hash] {
			t.Errorf("%s was offered for the film", byHash(t, got, hash).Torrent.Title)
		}
	}
	if !have["judas"] || !have["erai"] {
		t.Errorf("the film's own releases were dropped: kept %v", have)
	}
	if got.Best == nil || (got.Best.Torrent.InfoHash != "judas" && got.Best.Torrent.InfoHash != "erai") {
		t.Errorf("best = %+v, want the film", got.Best)
	}
}

// The same releases still reach the episodes they are.
func TestShowsEpisodesStillMatchBesideTheFilm(t *testing.T) {
	st := jjkStore(t)
	cases := []struct {
		anime, episode int
		want           []string
	}{
		{jjk1, 1, []string{"s1e01", "sp01"}},
		{jjk3, 2, []string{"s3e02"}},
	}
	for _, c := range cases {
		idx := &askedIndexer{results: jjkReleases()}
		f := NewFinder(st, idx, slog.New(slog.NewTextHandler(io.Discard, nil)))
		got, err := f.Find(context.Background(), Request{AnimeID: c.anime, Episode: c.episode})
		if err != nil {
			t.Fatal(err)
		}
		have := kept(got)
		for _, hash := range c.want {
			if !have[hash] {
				t.Errorf("anime %d episode %d lost %s; kept %v", c.anime, c.episode, hash, have)
			}
		}
		// The film names no episode, so it may stay as an unconfirmed pack; it must not be the pick.
		if got.Best == nil || !slices.Contains(c.want, got.Best.Torrent.InfoHash) {
			t.Errorf("anime %d episode %d picked %+v, want one of %v", c.anime, c.episode, got.Best, c.want)
		}
	}
}
