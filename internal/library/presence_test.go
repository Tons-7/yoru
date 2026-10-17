package library

import (
	"context"
	"testing"

	"yoru/internal/discord"
	"yoru/internal/store"
)

// fakeCard keeps what the Discord card was told.
type fakeCard struct {
	shown   []discord.Watching
	cleared int
}

func (f *fakeCard) SetEnabled(bool)         {}
func (f *fakeCard) Show(w discord.Watching) { f.shown = append(f.shown, w) }
func (f *fakeCard) Clear()                  { f.cleared++ }

// The card shows this machine's playback, others' only when allowed, never an adult title, and goes when it ends.
func TestPresenceReports(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	episodes, cover, english := 28, "https://img/frieren.jpg", "Frieren: Beyond Journey's End"
	if _, err := st.ImportList(ctx, []store.Anime{
		{ID: 154587, Romaji: "Sousou no Frieren", English: &english, Episodes: &episodes, Cover: &cover, Synonyms: "[]", Genres: "[]"},
		{ID: 999, Romaji: "Something Adult", IsAdult: true, Synonyms: "[]", Genres: "[]"},
	}, nil, store.ImportMerge); err != nil {
		t.Fatal(err)
	}
	card := &fakeCard{}
	p := &Presence{store: st, card: card}

	p.Report(ctx, 154587, 5, 60, 1440, false, false, true)
	if len(card.shown) != 1 {
		t.Fatalf("this machine's playback: %d shown", len(card.shown))
	}
	w := card.shown[0]
	if w.Title != english || w.Episode != "Episode 5 of 28" || w.Cover != cover ||
		w.Link != "https://anilist.co/anime/154587" || w.Position != 60 || w.Paused {
		t.Errorf("card %+v", w)
	}

	p.Report(ctx, 154587, 6, 10, 1440, false, false, false)
	if len(card.shown) != 1 {
		t.Error("a phone's playback showed while only this machine's should")
	}
	p.everywhere = true
	p.Report(ctx, 154587, 6, 10, 1440, true, false, false)
	if len(card.shown) != 2 || !card.shown[1].Paused {
		t.Errorf("with other devices allowed: %+v", card.shown)
	}

	p.Report(ctx, 999, 1, 10, 1440, false, false, true)
	if len(card.shown) != 2 || card.cleared != 1 {
		t.Errorf("an adult title: shown %d, cleared %d; want it kept off the card", len(card.shown), card.cleared)
	}
	p.Report(ctx, 154587, 6, 1440, 1440, false, true, true)
	if card.cleared != 2 {
		t.Errorf("the episode ended and the card stayed: cleared %d", card.cleared)
	}
	// A show not in the catalogue has nothing to show.
	p.Report(ctx, 42, 1, 1, 100, false, false, true)
	if len(card.shown) != 2 {
		t.Errorf("an unknown show was shown")
	}
	// Nor one without AniList's record, which is what says adult or not: a MAL-only title, or a show not yet loaded.
	for _, id := range []int{-424242, 777} {
		if err := st.EnsureAnime(ctx, id); err != nil {
			t.Fatal(err)
		}
		p.Report(ctx, id, 1, 1, 100, false, false, true)
	}
	if len(card.shown) != 2 {
		t.Errorf("a show of unknown adult status was shown: %+v", card.shown[2:])
	}
}
