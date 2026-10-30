package store

import (
	"context"
	"testing"
)

func TestAlertShowsFollowTheReleaseWatch(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, id := range []int{1, 2, 3, 4, 5} {
		seedAnime(t, s, id)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.SetFollow(ctx, 1, true))
	_, err := s.w.ExecContext(ctx, `INSERT INTO list_entry (id, anime_id, status, progress) VALUES (-1, 2, 'CURRENT', 3), (-2, 3, 'CURRENT', 1), (-3, 5, 'PLANNING', 0)`)
	must(err)
	must(s.SetAnimePref(ctx, 3, "notify.enabled", "false"))
	must(s.SetAnimePref(ctx, 4, "autodownload.enabled", "true"))
	must(s.SetFollow(ctx, 5, true))
	must(s.SetNotify(ctx, 5, false))

	shows, err := s.AlertShows(ctx)
	must(err)
	got := map[int]string{}
	for _, a := range shows {
		got[a.AnimeID] = a.Because
	}
	want := map[int]string{1: "followed", 2: "watching", 4: "auto"}
	if len(got) != len(want) {
		t.Fatalf("alert shows = %v, want %v", got, want)
	}
	for id, because := range want {
		if got[id] != because {
			t.Errorf("show %d: because %q, want %q", id, got[id], because)
		}
	}
	if shows[0].Title != "Show" {
		t.Errorf("title = %q", shows[0].Title)
	}
}

func TestResumePointsOnlyForPartWatchedEpisodes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedAnime(t, s, 1)
	for _, p := range []PlaybackState{
		{AnimeID: 1, EpKey: "4", Position: 600, Duration: 1440},
		{AnimeID: 1, EpKey: "5", Position: 1430, Duration: 1440},
	} {
		if _, err := s.SavePlayback(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	points, err := s.ResumePoints(ctx, []int{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := points[[2]int{1, 4}]; got != [2]float64{600, 1440} {
		t.Errorf("episode 4 resumes at %v, want 600 of 1440", got)
	}
	if _, ok := points[[2]int{1, 5}]; ok || len(points) != 1 {
		t.Errorf("a finished episode is offered to resume: %v", points)
	}
}

func TestReadyEpisodesFoundOrHeld(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedAnime(t, s, 1)
	seedAnime(t, s, 2)
	if err := s.RecordFetched(ctx, "hash", 1, 3); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordFetched(ctx, "other", 2, 9); err != nil {
		t.Fatal(err)
	}
	ready, err := s.ReadyEpisodes(ctx, []int{1})
	if err != nil {
		t.Fatal(err)
	}
	if !ready[[2]int{1, 3}] || ready[[2]int{1, 4}] || ready[[2]int{2, 9}] {
		t.Errorf("ready = %v, want only show 1 episode 3", ready)
	}
}

func TestWatchRowsCarryEachShowsOwnSwitches(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedAnime(t, s, 1)
	seedAnime(t, s, 2)
	if err := s.SetFollow(ctx, 1, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAnimePref(ctx, 2, "autodownload.enabled", "true"); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordFetched(ctx, "hash", 2, 4); err != nil {
		t.Fatal(err)
	}
	rows, err := s.WatchRows(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[int]WatchRow{}
	for _, r := range rows {
		got[r.AnimeID] = r
	}
	if len(got) != 2 || got[1].Auto != "" || got[2].Auto != "true" || got[2].First != 4 {
		t.Errorf("watch rows = %+v", rows)
	}
}

func TestShowCardsNameEachShow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedAnime(t, s, 7)
	cards, err := s.ShowCards(ctx, []int{7, 8})
	if err != nil {
		t.Fatal(err)
	}
	if cards[7].Title != "Show" {
		t.Errorf("card 7 = %+v", cards[7])
	}
	if _, ok := cards[8]; ok {
		t.Error("an unknown show got a card")
	}
}
