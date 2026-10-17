package library

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"yoru/internal/discord"
	"yoru/internal/store"
)

// Presence tells Discord what plays when Settings say so: this machine's playback, or every device's too.
// Adult titles never show.
type Presence struct {
	store *store.Store
	card  presenceCard

	mu         sync.Mutex
	everywhere bool
	// The last show reported, as reports come every few seconds for the same one.
	show struct {
		id    int
		title string
		a     store.Anime
	}
}

// presenceCard is the Discord card (discord.Presence), or a test's.
type presenceCard interface {
	SetEnabled(bool)
	Show(discord.Watching)
	Clear()
}

func NewPresence(st *store.Store, log *slog.Logger) *Presence {
	return &Presence{store: st, card: discord.New(log)}
}

// Apply takes the Discord settings: on or off, and whether other devices' playback shows too.
func (p *Presence) Apply(prefs store.Prefs) {
	p.card.SetEnabled(prefs.Bool("discord.enabled"))
	p.mu.Lock()
	p.everywhere = prefs.Bool("discord.everywhere")
	p.mu.Unlock()
}

// Report is one playback report; local when it came from this machine, final when the episode ended or closed.
func (p *Presence) Report(ctx context.Context, animeID, episode int, position, duration float64, paused, final, local bool) {
	if p == nil {
		return
	}
	p.mu.Lock()
	everywhere := p.everywhere
	p.mu.Unlock()
	if !local && !everywhere {
		return
	}
	if final {
		p.card.Clear()
		return
	}
	a, title, ok := p.lookup(ctx, animeID)
	if !ok || a.IsAdult {
		p.card.Clear()
		return
	}
	line := fmt.Sprintf("Episode %d", episode)
	if a.Episodes != nil && *a.Episodes >= episode {
		line += fmt.Sprintf(" of %d", *a.Episodes)
	}
	w := discord.Watching{Title: title, Episode: line, Position: position, Duration: duration, Paused: paused}
	if a.Cover != nil {
		w.Cover = *a.Cover
	}
	// Negative ids are MyAnimeList-only shows, with no AniList page.
	if animeID > 0 {
		w.Link = fmt.Sprintf("https://anilist.co/anime/%d", animeID)
	}
	p.card.Show(w)
}

// Stop clears the card, for a player closing.
func (p *Presence) Stop() {
	if p != nil {
		p.card.Clear()
	}
}

func (p *Presence) lookup(ctx context.Context, animeID int) (store.Anime, string, bool) {
	p.mu.Lock()
	if p.show.id == animeID && animeID != 0 {
		defer p.mu.Unlock()
		return p.show.a, p.show.title, true
	}
	p.mu.Unlock()
	a, _, ok, err := p.store.AnimeRecord(ctx, animeID)
	if err != nil || !ok {
		return store.Anime{}, "", false
	}
	title := store.PickTitle(p.store.TitleMode(ctx, animeID), a.Romaji, a.English)
	p.mu.Lock()
	p.show.id, p.show.title, p.show.a = animeID, title, a
	p.mu.Unlock()
	return a, title, true
}
