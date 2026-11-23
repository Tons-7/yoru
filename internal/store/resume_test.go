package store

import (
	"context"
	"testing"
)

func TestPeekAtTheEndingIsNotWatched(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedCatalogue(t, s, 1, "FINISHED", 12)

	if playThrough(t, s, 1, "2", 0, 300, 1435) {
		t.Fatal("five minutes in is not watched")
	}

	watched, err := s.SavePlayback(ctx, PlaybackState{
		AnimeID: 1, EpKey: "2", Position: 1420, Duration: 1435, Played: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if watched {
		t.Error("a seek to the end counted as watching the episode")
	}

	at, err := s.ResumeAt(ctx, 1, "2")
	if err != nil {
		t.Fatal(err)
	}
	if at != 300 {
		t.Errorf("resume at %v, want the 300s the viewer actually reached", at)
	}

	p, ok, err := s.LastInProgress(ctx, 1)
	if err != nil || !ok {
		t.Fatalf("expected an episode in progress, ok=%v err=%v", ok, err)
	}
	if p.EpKey != "2" || p.Position != 300 {
		t.Errorf("in progress = %+v", p)
	}
}

func TestPlayingThroughIsWatchedAndStartsOver(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedCatalogue(t, s, 1, "FINISHED", 12)

	if !playThrough(t, s, 1, "2", 0, 1435, 1435) {
		t.Fatal("an episode played end to end is watched")
	}
	at, err := s.ResumeAt(ctx, 1, "2")
	if err != nil {
		t.Fatal(err)
	}
	if at != 0 {
		t.Errorf("a finished episode should start over, got %v", at)
	}
	if _, ok, _ := s.LastInProgress(ctx, 1); ok {
		t.Error("a finished episode is not in progress")
	}
}

func TestSkippingOpeningAndEndingIsWatched(t *testing.T) {
	s := newTestStore(t)
	seedCatalogue(t, s, 1, "FINISHED", 12)

	playThrough(t, s, 1, "3", 0, 10, 1435)
	if _, err := s.SavePlayback(context.Background(), PlaybackState{
		AnimeID: 1, EpKey: "3", Position: 100, Duration: 1435,
	}); err != nil {
		t.Fatal(err)
	}
	if playThrough(t, s, 1, "3", 100, 1290, 1435) {
		t.Fatal("watched before the threshold")
	}
	watched, err := s.SavePlayback(context.Background(), PlaybackState{
		AnimeID: 1, EpKey: "3", Position: 1400, Duration: 1435,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !watched {
		t.Error("skipping the opening and ending should still count as watched")
	}
}

func TestWatchedEpisodeWoundBackResumesThere(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedCatalogue(t, s, 1, "FINISHED", 12)

	playThrough(t, s, 1, "2", 0, 1435, 1435)
	if _, err := s.SavePlayback(ctx, PlaybackState{
		AnimeID: 1, EpKey: "2", Position: 1127, Duration: 1435,
	}); err != nil {
		t.Fatal(err)
	}

	at, err := s.ResumeAt(ctx, 1, "2")
	if err != nil {
		t.Fatal(err)
	}
	if at != 1127 {
		t.Errorf("resume at %v, want 1127", at)
	}

	page, err := s.ContinueWatching(ctx, Paging{PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Resume == nil || page.Items[0].Resume.Position != 1127 {
		t.Errorf("continue watching should offer the wound-back episode: %+v", page.Items)
	}

	eps, err := s.Episodes(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range eps {
		if e.EpKey == "2" && !e.Resumable {
			t.Error("episode 2 holds a position worth resuming but is not flagged resumable")
		}
	}
}

func TestFirstReportNearTheEndIsKept(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedCatalogue(t, s, 1, "FINISHED", 12)

	if _, err := s.SavePlayback(ctx, PlaybackState{
		AnimeID: 1, EpKey: "5", Position: 1400, Duration: 1435, Played: 3,
	}); err != nil {
		t.Fatal(err)
	}
	at, err := s.ResumeAt(ctx, 1, "5")
	if err != nil {
		t.Fatal(err)
	}
	if at != 0 {
		t.Errorf("resume at %v; the credits are not a resume point", at)
	}
}
