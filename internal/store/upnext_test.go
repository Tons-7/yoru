package store

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func upNextOrder(t *testing.T, s *Store) string {
	t.Helper()
	items, err := s.UpNext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	for _, it := range items {
		parts = append(parts, fmt.Sprintf("%d:%d", it.AnimeID, it.Episode))
	}
	return strings.Join(parts, " ")
}

func upNextShows(t *testing.T, s *Store, entries ...Entry) {
	t.Helper()
	twelve := 12
	shows := []Anime{
		{ID: 1, Romaji: "One", Episodes: &twelve, Synonyms: "[]", Genres: "[]"},
		{ID: 2, Romaji: "Two", Episodes: &twelve, Synonyms: "[]", Genres: "[]"},
		{ID: 3, Romaji: "Three", Episodes: &twelve, Synonyms: "[]", Genres: "[]"},
	}
	if _, err := s.ImportList(context.Background(), shows, entries, ImportMerge); err != nil {
		t.Fatal(err)
	}
}

// A show's episodes stay together in episode order, and moving a show moves them all.
func TestUpNextGroupsByShow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	upNextShows(t, s)

	for _, p := range []Pick{{1, 5}, {2, 3}, {1, 6}, {1, 4}} {
		if ok, err := s.AddUpNext(ctx, p.AnimeID, p.Episode); err != nil || !ok {
			t.Fatalf("add %v: %v %v", p, ok, err)
		}
	}
	if got := upNextOrder(t, s); got != "1:4 1:5 1:6 2:3" {
		t.Fatalf("order %q", got)
	}
	if ok, _ := s.AddUpNext(ctx, 1, 5); ok {
		t.Error("an episode already there was added again")
	}

	if err := s.MoveUpNext(ctx, 2, 0); err != nil {
		t.Fatal(err)
	}
	if got := upNextOrder(t, s); got != "2:3 1:4 1:5 1:6" {
		t.Fatalf("after moving show 2 to the top: %q", got)
	}
	s.AddUpNext(ctx, 3, 1)
	s.MoveUpNext(ctx, 2, 99)
	if got := upNextOrder(t, s); got != "1:4 1:5 1:6 3:1 2:3" {
		t.Fatalf("after moving show 2 past the end: %q", got)
	}
	s.MoveUpNext(ctx, 3, 0)
	s.AddUpNext(ctx, 2, 4)
	if got := upNextOrder(t, s); got != "3:1 1:4 1:5 1:6 2:3 2:4" {
		t.Fatalf("an episode of a listed show joins it: %q", got)
	}

	s.RemoveUpNext(ctx, 1, 5)
	if got := upNextOrder(t, s); got != "3:1 1:4 1:6 2:3 2:4" {
		t.Fatalf("after removing 1:5: %q", got)
	}
	s.RemoveUpNext(ctx, 1, 0)
	if got := upNextOrder(t, s); got != "3:1 2:3 2:4" {
		t.Fatalf("after removing show 1: %q", got)
	}
	s.ClearUpNext(ctx)
	if got := upNextOrder(t, s); got != "" {
		t.Fatalf("after clearing: %q", got)
	}
}

// Watched episodes leave by themselves: played here, ticked, or watched elsewhere and pulled from the tracker.
func TestUpNextWatchedEpisodesLeave(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	current := "CURRENT"
	upNextShows(t, s, Entry{ID: 10, AnimeID: 1, Status: &current, Progress: 6, UpdatedAt: 100})

	for _, p := range []Pick{{1, 2}, {1, 7}, {1, 8}, {1, 9}, {2, 1}} {
		s.AddUpNext(ctx, p.AnimeID, p.Episode)
	}

	watched, err := s.SavePlayback(ctx, PlaybackState{AnimeID: 1, EpKey: "7", Position: 195, Duration: 200, Played: 120})
	if err != nil || !watched {
		t.Fatalf("watched %v, %v", watched, err)
	}
	if got := upNextOrder(t, s); got != "1:2 1:8 1:9 2:1" {
		t.Fatalf("after playing 7 to the end: %q", got)
	}
	// A short play is not a watch.
	s.SavePlayback(ctx, PlaybackState{AnimeID: 1, EpKey: "8", Position: 60, Duration: 200, Played: 60})
	if got := upNextOrder(t, s); got != "1:2 1:8 1:9 2:1" {
		t.Fatalf("a partly played episode left: %q", got)
	}

	if _, err := s.MarkWatched(ctx, 2, 1); err != nil {
		t.Fatal(err)
	}
	if got := upNextOrder(t, s); got != "1:2 1:8 1:9" {
		t.Fatalf("after ticking 2:1: %q", got)
	}

	// Watched elsewhere up to 9: 8 and 9 go; 2, added again to rewatch, stays.
	if _, err := s.ImportList(ctx, nil, []Entry{{ID: 10, AnimeID: 1, Status: &current, Progress: 9, UpdatedAt: 200}}, ImportMerge); err != nil {
		t.Fatal(err)
	}
	if got := upNextOrder(t, s); got != "1:2" {
		t.Fatalf("after the tracker said 9: %q", got)
	}
	s.SavePlayback(ctx, PlaybackState{AnimeID: 1, EpKey: "2", Position: 195, Duration: 200, Played: 120})
	if got := upNextOrder(t, s); got != "" {
		t.Fatalf("the rewatched episode stayed: %q", got)
	}
}

// Add next episodes takes one aired unwatched episode per show being watched, most recently watched first.
func TestFillUpNext(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	current, completed, repeating := "CURRENT", "COMPLETED", "REPEATING"
	upNextShows(t, s,
		Entry{ID: 10, AnimeID: 1, Status: &current, Progress: 2, UpdatedAt: 100},
		Entry{ID: 20, AnimeID: 2, Status: &repeating, Progress: 0, UpdatedAt: 100},
		Entry{ID: 30, AnimeID: 3, Status: &completed, Progress: 12, UpdatedAt: 100},
	)
	twelve, next, at := 12, 5, int(time.Now().Add(24*time.Hour).Unix())
	caughtUp := []Anime{
		{ID: 4, Romaji: "Airing", Episodes: &twelve, NextEpisode: &next, NextAiringAt: &at, Synonyms: "[]", Genres: "[]"},
		{ID: 5, Romaji: "Done", Episodes: &twelve, Synonyms: "[]", Genres: "[]"},
	}
	if _, err := s.ImportList(ctx, caughtUp, []Entry{
		{ID: 40, AnimeID: 4, Status: &current, Progress: 4, UpdatedAt: 100},
		{ID: 50, AnimeID: 5, Status: &current, Progress: 12, UpdatedAt: 100},
	}, ImportMerge); err != nil {
		t.Fatal(err)
	}
	// Show 2 was played last, so it comes first.
	s.SavePlayback(ctx, PlaybackState{AnimeID: 1, EpKey: "2", Position: 30, Duration: 200, Played: 30})
	s.w.ExecContext(ctx, `UPDATE playback SET last_played_at = last_played_at - 60 WHERE anime_id = 1`)
	s.SavePlayback(ctx, PlaybackState{AnimeID: 2, EpKey: "1", Position: 30, Duration: 200, Played: 30})

	added, err := s.FillUpNext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := upNextOrder(t, s); added != 2 || got != "2:1 1:3" {
		t.Fatalf("added %d: %q, want the rewatch's first episode then show 1's third", added, got)
	}
	if again, _ := s.FillUpNext(ctx); again != 0 {
		t.Errorf("a second press added %d", again)
	}
}

// After an episode the list plays top to bottom, past its end the last show's next, and when empty the show's own.
func TestPlayOrder(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	upNextShows(t, s)
	twelve, next, at := 12, 5, int(time.Now().Add(24*time.Hour).Unix())
	s.ImportList(ctx, []Anime{{ID: 4, Romaji: "Airing", Episodes: &twelve, NextEpisode: &next, NextAiringAt: &at,
		Synonyms: "[]", Genres: "[]"}}, nil, ImportMerge)

	order := func(animeID, episode, n int) string {
		t.Helper()
		picks, fromList, err := s.PlayOrder(ctx, animeID, episode, n)
		if err != nil {
			t.Fatal(err)
		}
		var parts []string
		for _, p := range picks {
			parts = append(parts, fmt.Sprintf("%d:%d", p.AnimeID, p.Episode))
		}
		return fmt.Sprintf("%s (%d from the list)", strings.Join(parts, " "), fromList)
	}

	if got := order(1, 5, 2); got != "1:6 1:7 (0 from the list)" {
		t.Errorf("empty list: %s", got)
	}
	for _, p := range []Pick{{1, 5}, {4, 5}, {3, 1}, {3, 2}} {
		s.AddUpNext(ctx, p.AnimeID, p.Episode)
	}
	// 1:5 is the one playing and 4:5 airs tomorrow, so neither plays next.
	if got := order(1, 5, 3); got != "3:1 3:2 3:3 (2 from the list)" {
		t.Errorf("from the list: %s", got)
	}
	if got := order(3, 1, 1); got != "1:5 (1 from the list)" {
		t.Errorf("playing the list's own episode: %s", got)
	}
}
