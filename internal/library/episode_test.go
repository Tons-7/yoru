package library

import (
	"slices"
	"testing"

	"yoru/internal/parse"
	"yoru/internal/store"
)

// An entry is one season from one, so omitting the season never yields a later season's episode 1.
func TestSeasonComesFromTheShowsOwnTitle(t *testing.T) {
	for _, tc := range []struct {
		title string
		want  int
	}{
		{"Sousou no Frieren", 1},
		{"Sousou no Frieren 2nd Season", 2},
		{"Kimetsu no Yaiba Season 3", 3},
		{"Bleach: Sennen Kessen-hen", 1},
	} {
		if got := parse.SeasonOf(tc.title); got != tc.want && !(tc.want == 1 && got == 0) {
			t.Errorf("%q: season %d, want %d", tc.title, got, tc.want)
		}
	}
}

// A first season's episode 1 rejects a release marked as a later season.
func TestLaterSeasonIsRejectedForAFirstSeasonRequest(t *testing.T) {
	rel := parse.Parse("[Judas] Frieren - S02E01.mkv")
	if rel.Season != 2 {
		t.Fatalf("parsed season %d, want 2", rel.Season)
	}
	if verifies(rel, Request{Episode: 1, Season: 1}) {
		t.Error("S02E01 should not satisfy season 1 episode 1")
	}
	if !verifies(rel, Request{Episode: 1, Season: 2}) {
		t.Error("S02E01 should satisfy season 2 episode 1")
	}
}

// Conan's 1218 is TheTVDB's S34E32; the show's own episode 32, from 1996, is not it.
func TestTvdbSeasonRestartIsNotTheShowsOwnNumber(t *testing.T) {
	req := Request{Episode: 1218, Season: 1, Alias: store.EpisodeAlias{Tvdb: 32, TvdbSeason: 34, StatedTvdb: true}}
	for _, tc := range []struct {
		title string
		want  bool
	}{
		{"Detective Conan S01E32 VOSTFR 1080p WEB x264 AAC -Tsundere-Raws (ADN) (Meitantei Conan,Case Closed)", false},
		{"[JnF] Meitantei Conan 030-032 [DVDRip]", false},
		{"[ShinkaDan] Знаменитый детектив Конан / Meitantei Conan 31-32 серия [RU-sub]", false},
		{"[SubsPlease] Detective Conan - 1218 (1080p) [845D5162].mkv", true},
	} {
		rel := parse.Parse(tc.title)
		if got := verifies(rel, req) && confirms(rel, req); got != tc.want {
			t.Errorf("taken as 1218: %v, want %v: %s", got, tc.want, tc.title)
		}
		if slices.Contains(numbersFor(rel, req), 32) {
			t.Errorf("a file numbered 32 would be picked from %s", tc.title)
		}
	}
}
