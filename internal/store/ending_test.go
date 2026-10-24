package store

import (
	"context"
	"testing"
)

// playWithEnding reports ten-second steps up to `to`, as a player that knows the ending's start would.
func playWithEnding(t *testing.T, s *Store, ep string, to, dur, ending float64) bool {
	t.Helper()
	watched := false
	for at := 0.0; ; at += 10 {
		report := min(at, to)
		w, err := s.SavePlayback(context.Background(), PlaybackState{
			AnimeID: 1, EpKey: ep, Position: report, Duration: dur, Played: min(10, report), Ending: ending,
		})
		if err != nil {
			t.Fatal(err)
		}
		watched = watched || w
		if report >= to {
			return watched
		}
	}
}

// Under "when the ending starts", closing in the credits is a finished episode, not a resume point.
func TestEndingStartCountsAsWatched(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedCatalogue(t, s, 1, "FINISHED", 12)
	s.SetListStatus(ctx, 1, StatusCurrent, -1)
	if err := s.SetSetting(ctx, "sync.progress_at", EndingThreshold); err != nil {
		t.Fatal(err)
	}

	// The ending starts at 84%, well before the 90% the share would ask for.
	if playWithEnding(t, s, "2", 1190, 1435, 1200) {
		t.Fatal("watched before the ending started")
	}
	if !playWithEnding(t, s, "2", 1210, 1435, 1200) {
		t.Fatal("ten seconds into the ending isn't watched")
	}
	if at, _ := s.ResumeAt(ctx, 1, "2"); at != 0 {
		t.Errorf("resume at %v; stopped in the credits should start over", at)
	}
	if _, ok, _ := s.LastInProgress(ctx, 1); ok {
		t.Error("an episode ticked at its ending is still in progress")
	}
	page, err := s.ContinueWatching(ctx, Paging{Page: 1, PerPage: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range page.Items {
		if r := it.Resume; r != nil && r.EpKey == "2" && r.Position > 0 {
			t.Errorf("continue watching offers the credits as a resume: %+v", r)
		}
	}
	eps, _ := s.Episodes(ctx, 1)
	for _, e := range eps {
		if e.EpKey == "2" && e.Resumable {
			t.Error("episode 2 is flagged resumable in its credits")
		}
	}

	// A rewatch stopped before the ending still resumes there.
	if _, err := s.SavePlayback(ctx, PlaybackState{AnimeID: 1, EpKey: "2", Position: 600, Duration: 1435}); err != nil {
		t.Fatal(err)
	}
	if at, _ := s.ResumeAt(ctx, 1, "2"); at != 600 {
		t.Errorf("rewatch resume at %v, want 600", at)
	}
}

// An ending too early to be the credits, or none known, leaves the 90% share.
func TestAnImplausibleEndingLeavesTheShare(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedCatalogue(t, s, 1, "FINISHED", 12)
	if err := s.SetAnimePref(ctx, 1, "sync.progress_at", EndingThreshold); err != nil {
		t.Fatal(err)
	}
	if playWithEnding(t, s, "3", 1260, 1435, 0) {
		t.Error("with no ending known, 88% ticked the episode")
	}

	// A song half way through isn't the credits.
	if playWithEnding(t, s, "4", 800, 1435, 700) {
		t.Error("an ending at 49% ticked the episode")
	}
	if !playWithEnding(t, s, "4", 1300, 1435, 700) {
		t.Error("past 90% with an implausible ending isn't watched")
	}
}

// The ending is only used when chosen; the 90% default ignores it.
func TestEndingIgnoredUnlessChosen(t *testing.T) {
	s := newTestStore(t)
	seedCatalogue(t, s, 1, "FINISHED", 12)
	if playWithEnding(t, s, "2", 1210, 1435, 1200) {
		t.Error("the ending ticked the episode under the 90% setting")
	}
}
