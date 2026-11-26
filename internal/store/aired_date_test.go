package store

import (
	"context"
	"testing"
	"time"

	"yoru/internal/metadata"
)

func TestAnAiredEpisodeWithAFutureListedDateKeepsOneDate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedCatalogue(t, s, 400, "RELEASING", 12)
	releasing, twelve, ten := "RELEASING", 12, 10
	at := int(time.Now().Add(72 * time.Hour).Unix())
	if _, err := s.ImportList(ctx, []Anime{{ID: 400, Romaji: "Show", Status: &releasing, Episodes: &twelve,
		NextEpisode: &ten, NextAiringAt: &at}}, nil, ImportMerge); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveEpisodes(ctx, 400, []metadata.Episode{
		{Number: 8, TitleEN: "Eight", AirDate: time.Now().Add(-5 * 24 * time.Hour).Unix()},
		{Number: 9, TitleEN: "Nine", AirDate: time.Now().Add(2 * time.Hour).Unix()},
	}); err != nil {
		t.Fatal(err)
	}

	nine := func() int64 {
		t.Helper()
		eps, err := s.Episodes(ctx, 400)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range eps {
			if e.Number == 9 && e.AirDate != nil {
				return *e.AirDate
			}
		}
		t.Fatal("episode 9 has no date")
		return 0
	}
	first := nine()
	time.Sleep(1100 * time.Millisecond)
	if again := nine(); again != first {
		t.Fatalf("episode 9's date moves with each request: %d then %d", first, again)
	}
	if want := int64(at) - 7*24*3600; first != want {
		t.Errorf("episode 9 aired %d, want a week before episode 10 (%d)", first, want)
	}
	if first > time.Now().Unix() {
		t.Error("episode 9 is dated in the future though episode 10 is next")
	}
}
