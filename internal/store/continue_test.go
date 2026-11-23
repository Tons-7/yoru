package store

import (
	"context"
	"testing"
	"time"
)

func TestContinueWatchingOffersTheNextEpisode(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	all := Paging{Page: 1, PerPage: 50}
	seedCatalogue(t, s, 1, "FINISHED", 12)

	if err := s.SetListStatus(ctx, 1, StatusCurrent, -1); err != nil {
		t.Fatal(err)
	}
	playThrough(t, s, 1, "1", 0, 1400, 1400)
	if _, err := s.MarkWatched(ctx, 1, 1); err != nil {
		t.Fatal(err)
	}

	page, err := s.ContinueWatching(ctx, all)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Total != 1 {
		t.Fatalf("a finished episode left nothing to continue: %d items, total %d", len(page.Items), page.Total)
	}
	if got := page.Items[0].Progress; got != 1 {
		t.Errorf("progress = %d, want 1 so the card offers episode 2", got)
	}
	if r := page.Items[0].Resume; r != nil && r.Position > 0 {
		t.Errorf("nothing to resume, yet a position came back: %+v", r)
	}

	playThrough(t, s, 1, "2", 0, 300, 1400)
	page, err = s.ContinueWatching(ctx, all)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("items = %d, want the one show", len(page.Items))
	}
	if r := page.Items[0].Resume; r == nil || r.EpKey != "2" || r.Position < 200 {
		t.Errorf("resume = %+v, want episode 2 part way", r)
	}
}

func TestContinueWatchingUsesTheShowsThreshold(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedCatalogue(t, s, 1, "FINISHED", 12)
	s.SetListStatus(ctx, 1, StatusCurrent, -1)
	s.MarkWatched(ctx, 1, 1)
	if err := s.SetAnimePref(ctx, 1, "sync.progress_at", "0.8"); err != nil {
		t.Fatal(err)
	}
	playThrough(t, s, 1, "2", 0, 1190, 1400)

	page, err := s.ContinueWatching(ctx, Paging{Page: 1, PerPage: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range page.Items {
		if r := it.Resume; r != nil && r.EpKey == "2" && r.Position > 0 {
			t.Fatalf("episode 2 at 85%% offered as a resume under a 0.8 threshold: %+v", r)
		}
	}
	if at, _ := s.ResumeAt(ctx, 1, "2"); at != 0 {
		t.Fatalf("ResumeAt = %v; the two rules disagree", at)
	}
}

func TestContinueWatchingSurvivesARewatch(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	all := Paging{Page: 1, PerPage: 50}
	seedCatalogue(t, s, 1, "FINISHED", 12)

	for _, ep := range []string{"1", "2", "3"} {
		playThrough(t, s, 1, ep, 0, 1400, 1400)
	}
	if err := s.SetListStatus(ctx, 1, "COMPLETED", -1); err != nil {
		t.Fatal(err)
	}

	if err := s.SetListStatus(ctx, 1, StatusRepeating, -1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkWatched(ctx, 1, 2); err != nil {
		t.Fatal(err)
	}

	page, err := s.ContinueWatching(ctx, all)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("a rewatch is missing from continue watching: %+v", page.Items)
	}
	if got := page.Items[0].Progress; got != 2 {
		t.Errorf("progress = %d, want 2 so it offers episode 3", got)
	}
}

func TestDismissingANeverPlayedEpisodeSticks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	all := Paging{Page: 1, PerPage: 50}
	seedCatalogue(t, s, 1, "FINISHED", 12)

	if err := s.SetListStatus(ctx, 1, StatusCurrent, -1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkWatched(ctx, 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.DismissResume(ctx, 1, "2"); err != nil {
		t.Fatal(err)
	}

	page, err := s.ContinueWatching(ctx, all)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 || page.Total != 0 {
		t.Fatalf("dismissed and still offered: %d items, total %d", len(page.Items), page.Total)
	}

	hist, err := s.History(ctx, all)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist.Items) != 0 || hist.Total != 0 {
		t.Errorf("the dismissal turned up in history: %+v", hist.Items)
	}
	recent, err := s.RecentlyWatched(ctx, all)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent.Items) != 0 {
		t.Errorf("the dismissal turned up in recently watched: %+v", recent.Items)
	}

	playThrough(t, s, 1, "2", 0, 300, 1400)
	page, err = s.ContinueWatching(ctx, all)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("playing the episode did not bring it back: %+v", page.Items)
	}
}

func TestContinueWatchingStopsAtTheEnd(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedCatalogue(t, s, 1, "FINISHED", 3)

	if err := s.SetListStatus(ctx, 1, StatusCurrent, -1); err != nil {
		t.Fatal(err)
	}
	playThrough(t, s, 1, "3", 0, 1400, 1400)
	if _, err := s.MarkWatched(ctx, 1, 3); err != nil {
		t.Fatal(err)
	}

	page, err := s.ContinueWatching(ctx, Paging{Page: 1, PerPage: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Errorf("every episode watched, yet still offered: %+v", page.Items)
	}
}

func TestEpisodeAiredReadsTheSchedule(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedCatalogue(t, s, 1, "RELEASING", 10)

	soon := int(time.Now().Add(48 * time.Hour).Unix())
	next := 8
	if _, err := s.ImportList(ctx, []Anime{
		{ID: 1, Romaji: "Show", NextEpisode: &next, NextAiringAt: &soon},
	}, nil, ImportMerge); err != nil {
		t.Fatal(err)
	}

	if ok, _ := s.EpisodeAired(ctx, 1, 7); !ok {
		t.Error("episode 7 is behind the next one, so it has aired")
	}
	ok, at := s.EpisodeAired(ctx, 1, 8)
	if ok {
		t.Error("episode 8 airs in two days, yet it counts as aired")
	}
	if at != int64(soon) {
		t.Errorf("airs at %d, want %d", at, soon)
	}
	if ok, _ := s.EpisodeAired(ctx, 1, 9); ok {
		t.Error("episode 9 is further out still")
	}
	seedCatalogue(t, s, 2, "FINISHED", 12)
	if ok, _ := s.EpisodeAired(ctx, 2, 3); !ok {
		t.Error("no schedule is not evidence of a future date")
	}
}

func TestContinueWatchingWaitsForTheEpisodeToAir(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	all := Paging{Page: 1, PerPage: 50}
	seedCatalogue(t, s, 1, "RELEASING", 12)

	soon := int(time.Now().Add(72 * time.Hour).Unix())
	next := 6
	if _, err := s.ImportList(ctx, []Anime{
		{ID: 1, Romaji: "Show", NextEpisode: &next, NextAiringAt: &soon},
	}, nil, ImportMerge); err != nil {
		t.Fatal(err)
	}
	if err := s.SetListStatus(ctx, 1, StatusCurrent, -1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkWatched(ctx, 1, 5); err != nil {
		t.Fatal(err)
	}

	page, err := s.ContinueWatching(ctx, all)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 || page.Total != 0 {
		t.Fatalf("episode 6 airs in three days, yet it is offered: %+v", page.Items)
	}

	past := int(time.Now().Add(-time.Hour).Unix())
	if _, err := s.ImportList(ctx, []Anime{
		{ID: 1, Romaji: "Show", NextEpisode: &next, NextAiringAt: &past},
	}, nil, ImportMerge); err != nil {
		t.Fatal(err)
	}
	page, err = s.ContinueWatching(ctx, all)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("the episode has aired, yet nothing to continue: %+v", page.Items)
	}
}

func TestDismissalIsNotAResumePoint(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedCatalogue(t, s, 1, "FINISHED", 12)

	if err := s.SetListStatus(ctx, 1, StatusCurrent, -1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkWatched(ctx, 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.DismissResume(ctx, 1, "2"); err != nil {
		t.Fatal(err)
	}

	page, err := s.Library(ctx, LibraryFilter{}, Paging{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range page.Items {
		if it.Resume != nil {
			t.Errorf("a dismissal came back as a resume point: %+v", it.Resume)
		}
	}
}
