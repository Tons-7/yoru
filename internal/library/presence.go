package library

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"yoru/internal/discord"
	"yoru/internal/store"
)

type Presence struct {
	store *store.Store
	card  presenceCard

	mu         sync.Mutex
	everywhere bool
	showing    [2]int
	show       struct {
		id    int
		title string
		a     store.Anime
	}
}

type presenceCard interface {
	SetEnabled(bool)
	Show(discord.Watching)
	Clear()
}

func NewPresence(st *store.Store, log *slog.Logger) *Presence {
	return &Presence{store: st, card: discord.New(log)}
}

func (p *Presence) Apply(prefs store.Prefs) {
	p.card.SetEnabled(prefs.Bool("discord.enabled"))
	p.mu.Lock()
	p.everywhere = prefs.Bool("discord.everywhere")
	p.mu.Unlock()
}

func (p *Presence) Report(ctx context.Context, animeID, episode int, position, duration float64, paused, final, local bool) {
	if p == nil {
		return
	}
	p.mu.Lock()
	everywhere := p.everywhere
	showing := local || p.showing == [2]int{animeID, episode}
	p.mu.Unlock()
	if !local && !everywhere {
		return
	}
	if final {
		if showing {
			p.forget()
			p.card.Clear()
		}
		return
	}
	a, title, ok := p.lookup(ctx, animeID)
	if !ok || a.IsAdult {
		if showing {
			p.forget()
			p.card.Clear()
		}
		return
	}
	p.mu.Lock()
	p.showing = [2]int{animeID, episode}
	p.mu.Unlock()
	line := fmt.Sprintf("Episode %d", episode)
	if a.Episodes != nil && *a.Episodes >= episode {
		line += fmt.Sprintf(" of %d", *a.Episodes)
	}
	w := discord.Watching{Title: title, Episode: line, Position: position, Duration: duration, Paused: paused}
	if a.Cover != nil {
		w.Cover = *a.Cover
	}
	if animeID > 0 {
		w.Link = fmt.Sprintf("https://anilist.co/anime/%d", animeID)
	}
	p.card.Show(w)
}

func (p *Presence) Stop() {
	if p != nil {
		p.forget()
		p.card.Clear()
	}
}

func (p *Presence) forget() {
	p.mu.Lock()
	p.showing = [2]int{}
	p.mu.Unlock()
}

func (p *Presence) lookup(ctx context.Context, animeID int) (store.Anime, string, bool) {
	p.mu.Lock()
	if p.show.id == animeID && animeID != 0 {
		a := p.show.a
		p.mu.Unlock()
		return a, store.PickTitle(p.store.TitleMode(ctx, animeID), a.Romaji, a.English), true
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
