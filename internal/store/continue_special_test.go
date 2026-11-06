package store

import (
	"context"
	"testing"
)

func continuing(t *testing.T, s *Store) []LibraryItem {
	t.Helper()
	page, err := s.ContinueWatching(context.Background(), Paging{Page: 1, PerPage: 50})
	if err != nil {
		t.Fatal(err)
	}
	return page.Items
}

func TestAResumeWithoutEpisodeRowsStillNamesItsEpisode(t *testing.T) {
	s := newTestStore(t)
	seedCatalogue(t, s, -62422, "FINISHED", 1)
	playThrough(t, s, -62422, "1", 0, 267, 1500)

	items := continuing(t, s)
	if len(items) != 1 || items[0].Resume == nil {
		t.Fatalf("items = %+v, want the special part way", items)
	}
	if ep := items[0].Resume.Episode; ep == nil || *ep != 1 {
		t.Errorf("resume episode = %v, want 1 from its ep_key", ep)
	}
}

func TestMarkingTheEpisodeWatchedTakesItOffTheRow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedCatalogue(t, s, -62422, "FINISHED", 1)
	playThrough(t, s, -62422, "1", 0, 267, 1500)

	if _, err := s.MarkWatched(ctx, -62422, 1); err != nil {
		t.Fatal(err)
	}
	if items := continuing(t, s); len(items) != 0 {
		t.Fatalf("still continuing after marking it watched: %+v", items[0].Resume)
	}

	playThrough(t, s, -62422, "1", 0, 300, 1500)
	if items := continuing(t, s); len(items) != 1 {
		t.Errorf("playing it again did not bring it back: %d items", len(items))
	}
}

func TestCompletingAShowTakesItsHalfPlayedEpisodeOffTheRow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedCatalogue(t, s, 7, "FINISHED", 12)
	if err := s.SetListStatus(ctx, 7, StatusCurrent, -1); err != nil {
		t.Fatal(err)
	}
	playThrough(t, s, 7, "4", 0, 300, 1400)
	if items := continuing(t, s); len(items) != 1 {
		t.Fatalf("items = %d, want the show part way", len(items))
	}

	if err := s.SetListStatus(ctx, 7, "COMPLETED", -1); err != nil {
		t.Fatal(err)
	}
	if items := continuing(t, s); len(items) != 0 {
		t.Errorf("a completed show stays on the row: %+v", items[0].Resume)
	}
}
