package store

import (
	"context"
	"testing"
)

func TestDerivedEpisodesDropNumbersPastTheEntry(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedCatalogue(t, s, 213657, "RELEASING", 6)

	if _, err := s.SaveDerivedEpisodes(ctx, 213657, []int{1, 2, 3, 14}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveDerivedEpisodes(ctx, 213657, []int{1, 2, 3}); err != nil {
		t.Fatal(err)
	}

	eps, err := s.Episodes(ctx, 213657)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 6 {
		t.Fatalf("got %d episodes, want 1-6", len(eps))
	}
	for i, e := range eps {
		if e.Number != i+1 {
			t.Errorf("episode %d is numbered %d", i+1, e.Number)
		}
	}
}

func TestDerivedEpisodesPruneEvenWhenNothingIsFound(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedCatalogue(t, s, 500, "RELEASING", 6)

	if _, err := s.SaveDerivedEpisodes(ctx, 500, []int{1, 14}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveDerivedEpisodes(ctx, 500, nil); err != nil {
		t.Fatal(err)
	}
	eps, err := s.Episodes(ctx, 500)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range eps {
		if e.Number > 6 {
			t.Errorf("episode %d is past the entry's 6", e.Number)
		}
	}
}
