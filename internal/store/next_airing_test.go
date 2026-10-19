package store

import (
	"context"
	"testing"
	"time"

	"yoru/internal/metadata"
)

func airingAnime(t *testing.T, nextEpisode int, airingAt int64) *Store {
	t.Helper()
	st := newTestStore(t)
	episodes := 13
	next := nextEpisode
	at := int(airingAt)
	if _, err := st.ImportList(context.Background(), []Anime{{
		ID: 7, Romaji: "Show", Synonyms: "[]", Genres: "[]",
		Episodes: &episodes, NextEpisode: &next, NextAiringAt: &at,
	}}, nil, ImportMerge); err != nil {
		t.Fatal(err)
	}
	return st
}

// The list agrees with the schedule: a day-only date can't call an aired episode upcoming, or the reverse.
func TestEpisodeListKeepsToTheSchedule(t *testing.T) {
	ctx := context.Background()
	now := time.Now().Unix()
	at := now + 6*24*3600

	st := airingAnime(t, 10, at)
	if _, err := st.SaveEpisodes(ctx, 7, []metadata.Episode{
		{Number: 8, AirDate: now - 7*24*3600},
		{Number: 9, AirDate: now + 6*3600},
		{Number: 10, AirDate: at + 9*3600},
		{Number: 11, AirDate: at - 3600},
		{Number: 12, AirDate: at + 7*24*3600},
	}); err != nil {
		t.Fatal(err)
	}

	eps, err := st.Episodes(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	airs := map[int]int64{}
	for _, e := range eps {
		if e.AirDate != nil {
			airs[e.Number] = *e.AirDate
		}
	}
	if airs[8] != now-7*24*3600 {
		t.Errorf("episode 8 airs %d, want its own date kept", airs[8])
	}
	if airs[9] > time.Now().Unix() {
		t.Errorf("episode 9 is behind the next one yet airs in %d s", airs[9]-now)
	}
	if airs[10] != at {
		t.Errorf("episode 10 airs %d, want the schedule's %d", airs[10], at)
	}
	if airs[11] != at {
		t.Errorf("episode 11 airs %d, want no earlier than the next one", airs[11])
	}
	if airs[12] != at+7*24*3600 {
		t.Errorf("episode 12 airs %d, want its own later date kept", airs[12])
	}
}

// Auto-next and prefetch skip, as the watcher does, an episode airing next Saturday.
func TestNextEpisodeWaitsForTheBroadcast(t *testing.T) {
	ctx := context.Background()

	st := airingAnime(t, 6, time.Now().Add(24*time.Hour).Unix())
	if got, err := st.NextEpisode(ctx, 7, 5); err != nil || got != 0 {
		t.Fatalf("NextEpisode = %d, %v, want nothing until it airs", got, err)
	}
	if got, _ := st.NextEpisode(ctx, 7, 3); got != 4 {
		t.Errorf("NextEpisode(3) = %d, want an episode already out", got)
	}

	st = airingAnime(t, 6, time.Now().Add(-time.Hour).Unix())
	if got, _ := st.NextEpisode(ctx, 7, 5); got != 6 {
		t.Errorf("NextEpisode = %d, want 6 once it has aired", got)
	}

	if err := st.SetAnimePref(ctx, 7, "playback.skip_filler", "true"); err != nil {
		t.Fatal(err)
	}
	st = airingAnime(t, 6, time.Now().Add(24*time.Hour).Unix())
	st.SetAnimePref(ctx, 7, "playback.skip_filler", "true")
	if got, _ := st.NextEpisode(ctx, 7, 5); got != 0 {
		t.Errorf("with filler skipping, NextEpisode = %d, want nothing until it airs", got)
	}
}
