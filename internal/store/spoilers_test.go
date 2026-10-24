package store

import (
	"context"
	"fmt"
	"testing"

	"yoru/internal/metadata"
)

func spoilerFlags(t *testing.T, s *Store) string {
	t.Helper()
	eps, err := s.Episodes(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	out := ""
	for _, e := range eps {
		if e.Spoiler {
			out += fmt.Sprint(e.Number)
		}
	}
	return out
}

// By default the episodes after the next one are held back; watched ones, and a finished show, never are.
func TestSpoilersHideWhatComesAfterTheNextEpisode(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedCatalogue(t, s, 1, "FINISHED", 5)
	var eps []metadata.Episode
	for n := 1; n <= 5; n++ {
		eps = append(eps, metadata.Episode{Number: n, TitleEN: fmt.Sprintf("Title %d", n), Overview: "what happens"})
	}
	if _, err := s.SaveEpisodes(ctx, 1, eps); err != nil {
		t.Fatal(err)
	}
	s.SetListStatus(ctx, 1, StatusCurrent, -1)
	if _, err := s.MarkWatched(ctx, 1, 2); err != nil {
		t.Fatal(err)
	}

	if got := spoilerFlags(t, s); got != "45" {
		t.Errorf("hidden %q, want 4 and 5 (3 is next)", got)
	}
	s.SetSetting(ctx, "display.spoilers", SpoilersUnwatched)
	if got := spoilerFlags(t, s); got != "345" {
		t.Errorf("every unwatched: hidden %q, want 345", got)
	}
	s.SetAnimePref(ctx, 1, "display.spoilers", SpoilersOff)
	if got := spoilerFlags(t, s); got != "" {
		t.Errorf("shown all for this show, yet %q hidden", got)
	}
	s.SetAnimePref(ctx, 1, "display.spoilers", "")
	s.SetListStatus(ctx, 1, "COMPLETED", -1)
	if got := spoilerFlags(t, s); got != "" {
		t.Errorf("a finished show hides %q", got)
	}
}
