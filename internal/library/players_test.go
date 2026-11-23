package library

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"yoru/internal/anilist"
	"yoru/internal/player"
	"yoru/internal/store"
)

type fakePlayer struct {
	events  chan player.Event
	stopped int
}

func (f *fakePlayer) Play(context.Context, player.Options) error { return nil }
func (f *fakePlayer) Events() <-chan player.Event                { return f.events }
func (f *fakePlayer) Stop()                                      { f.stopped++ }

func TestMpvEOFFollowsThePlayedShareRule(t *testing.T) {
	ctx := context.Background()
	run := func(positions []float64) int {
		st := prefetchStore(t)
		episodes := 12
		st.ImportList(ctx, []store.Anime{{ID: 9, Romaji: "Show", Synonyms: "[]", Genres: "[]", Episodes: &episodes}}, nil, store.ImportMerge)
		p := NewPlayback(st, nil, nil, &fakePlayer{}, t.TempDir(), discard()).
			WithSync(NewSync(st, anilist.New(discard()), discard()))

		pl := &fakePlayer{events: make(chan player.Event, len(positions)+1)}
		for _, pos := range positions {
			pl.events <- player.Event{Kind: player.EventPosition, Position: pos, Duration: 20}
		}
		pl.events <- player.Event{Kind: player.EventEnd, Reason: "eof"}
		close(pl.events)
		p.track(pl, 9, 1, 1, p.trackGen.Load(), 0)

		progress, _ := st.ListProgress(ctx)
		return progress[9]
	}

	if got := run([]float64{1, 19.5}); got != 0 {
		t.Errorf("seek to the end counted: progress = %d", got)
	}
	var through []float64
	for s := 1.0; s <= 19; s++ {
		through = append(through, s)
	}
	if got := run(through); got != 1 {
		t.Errorf("played through: progress = %d, want 1", got)
	}
}

func TestExternalPlayerFollowsThePreference(t *testing.T) {
	st := prefetchStore(t)
	ctx := context.Background()
	mpv, vlc := &fakePlayer{}, &fakePlayer{}
	p := NewPlayback(st, nil, nil, mpv, t.TempDir(), discard()).WithPlayer("vlc", vlc)

	if name, pl := p.external(ctx); name != "mpv" || pl != Player(mpv) {
		t.Errorf("default = %s", name)
	}
	if err := st.SetSetting(ctx, "playback.player", "vlc"); err != nil {
		t.Fatal(err)
	}
	if name, pl := p.external(ctx); name != "vlc" || pl != Player(vlc) {
		t.Errorf("vlc chosen, got %s", name)
	}
	if err := st.SetSetting(ctx, "playback.player", "browser"); err != nil {
		t.Fatal(err)
	}
	if name, _ := p.external(ctx); name != "mpv" {
		t.Errorf("browser has no desktop player; mpv stands in, got %s", name)
	}

	p.StopPlayers()
	if mpv.stopped != 1 || vlc.stopped != 1 {
		t.Errorf("stop reached mpv %d, vlc %d times", mpv.stopped, vlc.stopped)
	}
}

func TestEOFWithoutPositionsCountsAsWatched(t *testing.T) {
	ctx := context.Background()
	st := prefetchStore(t)
	episodes := 12
	st.ImportList(ctx, []store.Anime{{ID: 9, Romaji: "Show", Synonyms: "[]", Genres: "[]", Episodes: &episodes}}, nil, store.ImportMerge)
	p := NewPlayback(st, nil, nil, &fakePlayer{}, t.TempDir(), discard()).
		WithSync(NewSync(st, anilist.New(discard()), discard()))

	pl := &fakePlayer{events: make(chan player.Event, 1)}
	pl.events <- player.Event{Kind: player.EventEnd, Reason: "eof"}
	close(pl.events)
	p.track(pl, 9, 1, 1, p.trackGen.Load(), 0)

	if progress, _ := st.ListProgress(ctx); progress[9] != 1 {
		t.Fatalf("progress = %d after an end of file with no position reports, want 1", progress[9])
	}
}

type recordingPlayer struct {
	fakePlayer
	mu    sync.Mutex
	plays []player.Options
}

func (r *recordingPlayer) Play(_ context.Context, o player.Options) error {
	r.mu.Lock()
	r.plays = append(r.plays, o)
	r.mu.Unlock()
	return nil
}

func (r *recordingPlayer) played() []player.Options {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]player.Options(nil), r.plays...)
}

func TestAutoNextStartsTheNextEpisode(t *testing.T) {
	ctx := context.Background()
	st := prefetchStore(t)
	episodes := 12
	st.ImportList(ctx, []store.Anime{{ID: 9, Romaji: "Show", Synonyms: "[]", Genres: "[]", Episodes: &episodes}}, nil, store.ImportMerge)
	dir := t.TempDir()
	var files []store.LocalFile
	for ep := 1; ep <= 2; ep++ {
		path := filepath.Join(dir, fmt.Sprintf("Show - %02d.mkv", ep))
		os.WriteFile(path, make([]byte, 1024), 0o644)
		files = append(files, store.LocalFile{Path: path, Size: 1024, AnimeID: 9, Episode: ep, Season: 1, Confidence: 1})
	}
	if _, err := st.SaveLocalFiles(ctx, 9, files); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(ctx, "playback.autonext", "true"); err != nil {
		t.Fatal(err)
	}
	mpv := &recordingPlayer{fakePlayer: fakePlayer{events: make(chan player.Event)}}
	p := NewPlayback(st, nil, nil, mpv, t.TempDir(), discard()).
		WithSync(NewSync(st, anilist.New(discard()), discard()))

	pl := &fakePlayer{events: make(chan player.Event, 1)}
	pl.events <- player.Event{Kind: player.EventEnd, Reason: "eof"}
	close(pl.events)
	p.track(pl, 9, 1, 1, p.trackGen.Load(), 0)

	deadline := time.Now().Add(5 * time.Second)
	for len(mpv.played()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	plays := mpv.played()
	if len(plays) != 1 || !strings.Contains(plays[0].Title, "2") {
		t.Fatalf("plays after episode 1 ended: %+v, want episode 2", plays)
	}
}
