package library

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"yoru/internal/indexer"
	"yoru/internal/parse"
	"yoru/internal/score"
	"yoru/internal/store"
)

// Watcher polls followed shows for new episodes, raises notifications, and optionally grabs them.
type Watcher struct {
	store   *store.Store
	finder  *Finder
	indexer indexer.Source
	queue   *Downloader
	log     *slog.Logger

	// waiting: episodes found only in audio the notifications don't ask for, and when last searched.
	waitMu  sync.Mutex
	waiting map[[2]int]time.Time
}

// How long such an episode waits before it is searched again: a dub comes days after the sub, and searching
// it every poll spends each site's budget on nothing. A var so tests need not wait.
var recheckWanted = 6 * time.Hour

// waitingFor reports an episode searched lately without what the notifications want; seen marks it so.
func (w *Watcher) waitingFor(animeID, episode int) bool {
	w.waitMu.Lock()
	defer w.waitMu.Unlock()
	at, ok := w.waiting[[2]int{animeID, episode}]
	return ok && time.Since(at) < recheckWanted
}

func (w *Watcher) setWaiting(animeID, episode int, waiting bool) {
	w.waitMu.Lock()
	defer w.waitMu.Unlock()
	key := [2]int{animeID, episode}
	if !waiting {
		delete(w.waiting, key)
		return
	}
	if w.waiting == nil {
		w.waiting = map[[2]int]time.Time{}
	}
	w.waiting[key] = time.Now()
}

func NewWatcher(s *store.Store, f *Finder, idx indexer.Source, log *slog.Logger) *Watcher {
	return &Watcher{store: s, finder: f, indexer: idx, log: log}
}

// WithDownloader enables auto-download. Without it the watcher only notifies.
func (w *Watcher) WithDownloader(d *Downloader) *Watcher { w.queue = d; return w }

type WatchReport struct {
	Checked    int `json:"checked"`
	Found      int `json:"found"`
	Notified   int `json:"notified"`
	Downloaded int `json:"downloaded"`
}

func (w *Watcher) Poll(ctx context.Context) (WatchReport, error) {
	var rep WatchReport

	prefs, err := w.store.Prefs(ctx, 0)
	if err != nil {
		return rep, err
	}
	// Notifications and auto-download are separate; either is reason to look.
	notifyOn := prefs.Bool("notify.enabled")
	want := audioFilter(prefs.String("notify.releases"))

	follows, err := w.store.Follows(ctx)
	if err != nil {
		return rep, err
	}

	for _, f := range follows {
		auto := w.autoDownload(ctx, f.AnimeID)
		// The global switch, which a show's own page can turn off for that show alone.
		notifyOn := notifyOn
		if p, err := w.store.Prefs(ctx, f.AnimeID); err == nil {
			notifyOn = p.Bool("notify.enabled")
		}
		if !notifyOn && !auto {
			continue
		}
		rep.Checked++

		// News is the last episode aired and only that: the backlog is known, a long-finished show is silent.
		latest := f.Latest(time.Now().Unix())
		if latest <= f.Progress || latest > f.Progress+maxBacklog || (f.Episodes > 0 && latest > f.Episodes) {
			continue
		}
		if seen, _ := w.store.AlreadyNotified(ctx, "", f.AnimeID, latest); seen || w.waitingFor(f.AnimeID, latest) {
			continue
		}
		w.check(ctx, f.AnimeID, latest, notifyOn, auto, want, &rep)
	}
	return rep, nil
}

// How far past progress an episode can be and still be news; beyond it the catalogue is suspect.
const maxBacklog = 200

func (w *Watcher) check(ctx context.Context, animeID, episode int, notifyOn, auto bool, want func(parse.Release) bool, rep *WatchReport) {
	found, err := w.finder.Find(ctx, Request{
		AnimeID: animeID,
		Episode: episode,
		// Season left to the finder, which reads it from the title.
		Prefs: w.prefsFor(ctx, animeID),
	})
	if err != nil {
		w.log.Warn("watch poll", "anime", animeID, "episode", episode, "err", err)
		return
	}

	// Confirmed only, or a guessed pack announces an unreleased episode; notify and fetch filter separately.
	var announce, fetch *score.Result
	for i := range found.Results {
		r := &found.Results[i]
		if !r.AutoPick || !r.Confirmed {
			continue
		}
		if seen, _ := w.store.AlreadyNotified(ctx, r.Torrent.InfoHash, animeID, episode); seen {
			continue
		}
		if fetch == nil {
			fetch = r
		}
		if announce == nil && want(r.Release) {
			announce = r
		}
	}
	if fetch == nil {
		return
	}
	rep.Found++
	// Downloaded once per episode, whichever release (or later announcement) comes first.
	fetchedBefore := w.store.EpisodeFetched(ctx, animeID, episode)

	switch {
	case !notifyOn:
		if err := w.store.RecordNotified(ctx, fetch.Torrent.InfoHash, animeID, episode); err != nil {
			w.log.Warn("record release", "err", err)
		}
	case announce == nil:
		// Nothing the notifications ask for yet (a sub, when they want a dub): it stays news for a later one.
		if err := w.store.RecordFetched(ctx, fetch.Torrent.InfoHash, animeID, episode); err != nil {
			w.log.Warn("record release", "err", err)
		}
		w.setWaiting(animeID, episode, true)
	default:
		w.setWaiting(animeID, episode, false)
		if err := w.notify(ctx, animeID, episode, announce.Torrent.Title, announce.Torrent.InfoHash); err != nil {
			w.log.Warn("notify", "err", err)
		} else {
			rep.Notified++
		}
	}

	if auto && !fetchedBefore && w.download(ctx, animeID, episode) {
		rep.Downloaded++
	}
}

// Per-anime prefs overlay the global switch.
func (w *Watcher) autoDownload(ctx context.Context, animeID int) bool {
	prefs, err := w.store.Prefs(ctx, animeID)
	return err == nil && prefs.Bool("autodownload.enabled")
}

// Queued, so many shows updating at once don't start as many downloads, and behind manual ones.
func (w *Watcher) download(ctx context.Context, animeID, episode int) bool {
	if w.queue == nil {
		return false
	}
	// Season 0: the finder derives it from the title, as the manual path does.
	// Forcing 1 makes it reject every season-2 release it just announced.
	if _, err := w.store.Enqueue(ctx, animeID, 0, []int{episode}); err != nil {
		w.log.Warn("auto-download", "anime", animeID, "episode", episode, "err", err)
		return false
	}
	w.queue.Wake()

	w.log.Info("auto-download queued", "anime", animeID, "episode", episode)
	return true
}

func (w *Watcher) notify(ctx context.Context, animeID, episode int, title, infoHash string) error {
	if _, err := w.store.AddNotification(ctx, store.Notification{
		Kind:    store.NotifyRelease,
		AnimeID: &animeID,
		Episode: &episode,
		Title:   fmt.Sprintf("Episode %d is available", episode),
		Body:    title,
		Payload: map[string]any{"infoHash": infoHash},
	}); err != nil {
		return err
	}
	return w.store.RecordNotified(ctx, infoHash, animeID, episode)
}

func (w *Watcher) prefsFor(ctx context.Context, animeID int) scorePrefs {
	p, err := w.store.Prefs(ctx, animeID)
	if err != nil {
		return defaultScorePrefs()
	}
	return prefsFromSettings(p)
}

// A dual-audio release satisfies both preferences; the track is chosen at playback.
func audioFilter(pref string) func(parse.Release) bool {
	switch strings.ToLower(pref) {
	case "dub":
		return func(r parse.Release) bool {
			return r.DualAudio || mentionsDub(r)
		}
	case "either", "both":
		return func(parse.Release) bool { return true }
	default:
		// Subbed releases carry no marker, so only an explicitly dub-only one is excluded.
		return func(r parse.Release) bool {
			return r.DualAudio || !mentionsDub(r)
		}
	}
}

// The original filename, as Title has lost bracketed tags like "[Dual Audio]".
func mentionsDub(r parse.Release) bool {
	lower := strings.ToLower(r.Raw)
	return strings.Contains(lower, "dub") || strings.Contains(lower, "english audio")
}
