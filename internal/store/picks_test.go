package store

import (
	"context"
	"slices"
	"testing"
)

// A finished show's sequel not started is missed: off the list first, then planned; not one started, dropped,
// unaired, or after a show still being watched.
func TestSequelsMissed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	str := func(v string) *string { return &v }
	shows := []Anime{
		{ID: 1, Romaji: "Finished", Status: str("FINISHED")},
		{ID: 2, Romaji: "Its sequel", Status: str("FINISHED")},
		{ID: 3, Romaji: "Its film, planned", Status: str("FINISHED"), Format: str("MOVIE")},
		{ID: 4, Romaji: "Started", Status: str("RELEASING")},
		{ID: 5, Romaji: "Announced", Status: str("NOT_YET_RELEASED")},
		{ID: 6, Romaji: "Dropped", Status: str("FINISHED")},
		{ID: 7, Romaji: "Watching", Status: str("RELEASING")},
		{ID: 8, Romaji: "After the one watching", Status: str("FINISHED")},
	}
	entries := []Entry{
		{ID: 1, AnimeID: 1, Status: str("COMPLETED")},
		{ID: 3, AnimeID: 3, Status: str("PLANNING")},
		{ID: 4, AnimeID: 4, Status: str("CURRENT")},
		{ID: 6, AnimeID: 6, Status: str("DROPPED")},
		{ID: 7, AnimeID: 7, Status: str("CURRENT")},
	}
	if _, err := s.ImportList(ctx, shows, entries, ImportMerge); err != nil {
		t.Fatal(err)
	}
	var rels []Relation
	for _, sequel := range []int{2, 3, 4, 5, 6} {
		rels = append(rels, Relation{AnimeID: 1, RelatedID: sequel, Kind: "SEQUEL"})
	}
	rels = append(rels, Relation{AnimeID: 7, RelatedID: 8, Kind: "SEQUEL"})
	if err := s.SaveRelations(ctx, []int{1, 7}, rels); err != nil {
		t.Fatal(err)
	}
	ids, err := s.SequelsMissed(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids, []int{2, 3}) {
		t.Errorf("missed = %v, want [2 3]: the one off the list, then the planned film", ids)
	}
}
