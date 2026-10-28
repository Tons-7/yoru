package store

import (
	"context"
	"testing"

	"yoru/internal/metadata"
)

// An episode's score and thread show once it's watched, never before; and they're due for a show
// with something watched until saved, then not again within the week.
func TestEpisodeScoresShowOnceWatched(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedFillerShow(t, s)

	if s.ScoresDue(ctx, 1, 900) {
		t.Fatal("scores due for a show with nothing watched")
	}
	playThrough(t, s, 1, "1", 0, 1400, 1400)
	if !s.ScoresDue(ctx, 1, 900) {
		t.Fatal("scores not due once an episode is watched")
	}
	forum := "https://myanimelist.net/forum/?topicid=1"
	if err := s.SaveScores(ctx, 900, []metadata.EpisodeScore{
		{Episode: 1, Score: 4.2, Forum: forum}, {Episode: 2, Score: 3.1},
	}); err != nil {
		t.Fatal(err)
	}
	if s.ScoresDue(ctx, 1, 900) {
		t.Error("scores due again right after saving")
	}

	eps, err := s.Episodes(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if e := eps[0]; e.Score == nil || *e.Score != 4.2 || e.Forum == nil || *e.Forum != forum {
		t.Errorf("watched episode 1: score %v forum %v, want 4.2 and the thread", e.Score, e.Forum)
	}
	if e := eps[1]; e.Score != nil || e.Forum != nil {
		t.Errorf("unwatched episode 2 gives away score %v forum %v", e.Score, e.Forum)
	}
}
