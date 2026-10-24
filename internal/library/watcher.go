package library

import (
	"cmp"
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
	// lookedAt: when a taken episode was last searched for a better release.
	lookedAt map[[2]int]time.Time
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
	w.announceDelays(ctx, follows, notifyOn, &rep)

	for _, f := range follows {
		auto := w.autoDownload(ctx, f.AnimeID)
		// The global switch, which a show's page can turn off for it alone; and one episode asked for from the
		// player, downloaded when out without auto-downloading the rest.
		notifyOn, wanted := notifyOn, 0
		if p, err := w.store.Prefs(ctx, f.AnimeID); err == nil {
			notifyOn, wanted = p.Bool("notify.enabled"), p.Int("autodownload.want")
		}
		if !notifyOn && !auto && wanted == 0 {
			continue
		}
		rep.Checked++

		// News is the last episode aired and only that: the backlog is known, a long-finished show is silent.
		latest := f.Latest(time.Now().Unix())
		if auto {
			w.settle(ctx, f, latest, &rep)
		}
		// The asked episode itself once aired, not the latest: a raw-only week or Yoru being off parts them.
		if wanted > 0 && wanted <= latest {
			w.fetchWanted(ctx, f.AnimeID, wanted, notifyOn, want, &rep)
		}
		if latest <= f.Progress || latest > f.Progress+maxBacklog || (f.Episodes > 0 && latest > f.Episodes) {
			continue
		}
		if auto {
			w.catchUp(ctx, f, latest, want, &rep)
		}
		if seen, _ := w.store.AlreadyNotified(ctx, "", f.AnimeID, latest); seen || w.waitingFor(f.AnimeID, latest) {
			continue
		}
		w.check(ctx, f.AnimeID, latest, notifyOn, auto, want, &rep)
	}
	w.dropOrphanWaits(ctx, follows)
	return rep, nil
}

// fetchWanted downloads the one episode asked for from the player, and forgets the ask once it's taken.
func (w *Watcher) fetchWanted(ctx context.Context, animeID, episode int, notifyOn bool, want func(parse.Release) bool, rep *WatchReport) {
	forget := func() { w.store.SetAnimePref(ctx, animeID, "autodownload.want", "") }
	switch {
	case w.store.HeldEpisode(ctx, animeID, episode):
		forget()
	case !w.store.EpisodeFetched(ctx, animeID, episode):
		// Found and confirmed first: queued blind, a raw-only episode would only fail in the queue.
		w.check(ctx, animeID, episode, notifyOn, true, want, rep)
		if w.store.EpisodeFetched(ctx, animeID, episode) {
			forget()
		}
	default:
		// Announced before it was asked for, so check would take it as done.
		if w.download(ctx, animeID, episode, nil) {
			rep.Downloaded++
			forget()
		}
	}
}

// dropOrphanWaits ends holds no settle will reach: auto-download turned off, or the show no longer followed.
func (w *Watcher) dropOrphanWaits(ctx context.Context, follows []store.Follow) {
	waits, err := w.store.Waits(ctx, 0)
	if err != nil || len(waits) == 0 {
		return
	}
	followed := map[int]bool{}
	for _, f := range follows {
		followed[f.AnimeID] = true
	}
	for _, h := range waits {
		if !followed[h.AnimeID] || !w.autoDownload(ctx, h.AnimeID) {
			w.store.EndWait(ctx, h.AnimeID, h.Episode)
		}
	}
}

// announceDelays tells of followed shows' episodes AniList moved later; the rest are let go, said to no one.
func (w *Watcher) announceDelays(ctx context.Context, follows []store.Follow, notifyOn bool, rep *WatchReport) {
	delays, err := w.store.UnannouncedDelays(ctx)
	if err != nil || len(delays) == 0 {
		return
	}
	followed := map[int]bool{}
	for _, f := range follows {
		followed[f.AnimeID] = true
	}
	for _, d := range delays {
		on := notifyOn
		if p, err := w.store.Prefs(ctx, d.AnimeID); err == nil {
			on = p.Bool("notify.enabled")
		}
		if followed[d.AnimeID] && on {
			body := "No new date yet"
			payload := map[string]any{"wasAt": d.WasAt}
			if d.NowAt != nil {
				body = "Now " + time.Unix(*d.NowAt, 0).Format("Mon 2 Jan, 15:04")
				payload["nowAt"] = *d.NowAt
			}
			animeID, episode := d.AnimeID, d.Episode
			if _, err := w.store.AddNotification(ctx, store.Notification{
				Kind: store.NotifyDelay, AnimeID: &animeID, Episode: &episode,
				Title: fmt.Sprintf("%s · Episode %d delayed", cmp.Or(w.store.ShownTitle(ctx, animeID), "A show you follow"), episode),
				Body:  body, Payload: payload,
			}); err != nil {
				w.log.Warn("announce delay", "anime", d.AnimeID, "err", err)
				continue
			}
			rep.Notified++
		}
		if err := w.store.MarkDelayAnnounced(ctx, d.AnimeID, d.Episode); err != nil {
			w.log.Warn("mark delay", "anime", d.AnimeID, "err", err)
		}
	}
}

// How far past progress an episode can be and still be news; beyond it the catalogue is suspect.
const maxBacklog = 200

// Earlier episodes one poll fetches per show, so a long absence costs the sites a few searches a run.
const catchUpPerPoll = 4

// catchUp queues what the watch missed since it first saw the show, before the latest, oldest first and with
// no bell; a backlog from before then is Download all's. One found nowhere waits like an unwanted one.
func (w *Watcher) catchUp(ctx context.Context, f store.Follow, latest int, want func(parse.Release) bool, rep *WatchReport) {
	first := w.store.FirstRecorded(ctx, f.AnimeID)
	if first == 0 {
		return
	}
	tried := 0
	for ep := max(f.Progress, first) + 1; ep < latest && tried < catchUpPerPoll; ep++ {
		if w.store.EpisodeFetched(ctx, f.AnimeID, ep) || w.waitingFor(f.AnimeID, ep) || w.store.HeldEpisode(ctx, f.AnimeID, ep) {
			continue
		}
		tried++
		w.check(ctx, f.AnimeID, ep, false, true, want, rep)
		if !w.store.EpisodeFetched(ctx, f.AnimeID, ep) {
			w.setWaiting(f.AnimeID, ep, true)
		}
	}
}

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

	// Held while the show waits for a preferred group or a higher resolution; settle takes it after.
	if auto && !fetchedBefore && !w.holding(ctx, animeID, episode, *fetch) && w.download(ctx, animeID, episode, nil) {
		rep.Downloaded++
	}
}

// asked is the episode the player asked to download when it's out, 0 for none.
func (w *Watcher) asked(ctx context.Context, animeID int) int {
	p, err := w.store.Prefs(ctx, animeID)
	if err != nil {
		return 0
	}
	return p.Int("autodownload.want")
}

// waitFor is how long the show's auto-download holds a new episode for a better release; 0 takes the first.
func (w *Watcher) waitFor(ctx context.Context, animeID int) time.Duration {
	p, err := w.store.Prefs(ctx, animeID)
	if err != nil {
		return 0
	}
	return time.Duration(max(0, p.Int64("autodownload.wait_hours"))) * time.Hour
}

// holding starts or continues the episode's wait for a better release than best; true while it lasts.
func (w *Watcher) holding(ctx context.Context, animeID, episode int, best score.Result) bool {
	// An episode asked for from the player is taken as found: only auto-download's own picks wait.
	wait := w.waitFor(ctx, animeID)
	if wait == 0 || !w.autoDownload(ctx, animeID) || w.asked(ctx, animeID) == episode ||
		score.Ideal(best, w.prefsFor(ctx, animeID)) {
		return false
	}
	since, err := w.store.WaitSince(ctx, animeID, episode)
	if err != nil {
		w.log.Warn("hold for a better release", "anime", animeID, "episode", episode, "err", err)
		return false
	}
	if time.Since(since) >= wait {
		w.store.EndWait(ctx, animeID, episode)
		return false
	}
	return true
}

// How long after taking a release the watch looks for a better one, and how often: a v2 lands within a day.
const (
	lookAgainFor   = 48 * time.Hour
	lookAgainEvery = 6 * time.Hour
)

// settle takes held episodes when their wait ends or the release they wait for lands. And while a taken episode
// is unwatched, it swaps it for the group's fixed v2, or, with a wait set, for a release that ranks above it.
func (w *Watcher) settle(ctx context.Context, f store.Follow, latest int, rep *WatchReport) {
	prefs := w.prefsFor(ctx, f.AnimeID)
	wait := w.waitFor(ctx, f.AnimeID)
	waits, err := w.store.Waits(ctx, f.AnimeID)
	if err != nil {
		w.log.Warn("read release waits", "anime", f.AnimeID, "err", err)
	}
	held := map[int]bool{}
	for _, h := range waits {
		held[h.Episode] = true
		// Watched, or taken another way meanwhile (played, picked by hand): nothing left to wait for.
		if h.Episode <= f.Progress || w.store.HeldEpisode(ctx, f.AnimeID, h.Episode) {
			w.store.EndWait(ctx, f.AnimeID, h.Episode)
			continue
		}
		ranked := w.find(ctx, f.AnimeID, h.Episode, prefs)
		best := firstConfirmed(ranked)
		over := time.Since(h.FirstSeen) >= wait
		if !over && (best == nil || !score.Ideal(*best, prefs)) {
			continue
		}
		if w.download(ctx, f.AnimeID, h.Episode, best) {
			rep.Downloaded++
		}
		w.store.EndWait(ctx, f.AnimeID, h.Episode)
	}

	for ep := max(f.Progress+1, latest-3); ep <= latest; ep++ {
		if held[ep] || !w.lookDue(f.AnimeID, ep) {
			continue
		}
		at, ok := w.store.TakenAt(ctx, f.AnimeID, ep)
		if !ok || time.Since(at) > lookAgainFor {
			continue
		}
		taken, ok, err := w.store.TorrentForEpisode(ctx, f.AnimeID, epKey(ep))
		// Only auto-download's own take is swapped: not one picked by hand, nor one already being watched.
		if err != nil || !ok || taken.Manual {
			continue
		}
		if at, _ := w.store.ResumeAt(ctx, f.AnimeID, epKey(ep)); at > 0 {
			continue
		}
		w.looked(f.AnimeID, ep)
		ranked := w.find(ctx, f.AnimeID, ep, prefs)
		best := firstConfirmed(ranked)
		if best == nil || strings.EqualFold(best.Torrent.InfoHash, taken.InfoHash) {
			continue
		}
		takenRel := parse.Parse(taken.Name)
		if !score.Fixes(*best, takenRel) && (wait == 0 || !score.Better(*best, taken.InfoHash, takenRel, ranked, prefs)) {
			continue
		}
		w.log.Info("a better release replaces the one taken", "anime", f.AnimeID, "episode", ep,
			"was", taken.Name, "now", best.Torrent.Title)
		if err := w.store.RecordFetched(ctx, best.Torrent.InfoHash, f.AnimeID, ep); err != nil {
			w.log.Warn("record release", "err", err)
		}
		if w.download(ctx, f.AnimeID, ep, best) {
			rep.Downloaded++
		}
	}
}

// find ranks the episode's releases, nil when the sites can't be asked.
func (w *Watcher) find(ctx context.Context, animeID, episode int, prefs scorePrefs) []score.Result {
	found, err := w.finder.Find(ctx, Request{AnimeID: animeID, Episode: episode, Prefs: prefs})
	if err != nil {
		w.log.Warn("watch poll", "anime", animeID, "episode", episode, "err", err)
		return nil
	}
	return found.Results
}

// firstConfirmed is the best release that states the episode and may start on its own.
func firstConfirmed(ranked []score.Result) *score.Result {
	for i := range ranked {
		if ranked[i].AutoPick && ranked[i].Confirmed {
			return &ranked[i]
		}
	}
	return nil
}

func (w *Watcher) lookDue(animeID, episode int) bool {
	w.waitMu.Lock()
	defer w.waitMu.Unlock()
	return time.Since(w.lookedAt[[2]int{animeID, episode}]) >= lookAgainEvery
}

func (w *Watcher) looked(animeID, episode int) {
	w.waitMu.Lock()
	defer w.waitMu.Unlock()
	if w.lookedAt == nil {
		w.lookedAt = map[[2]int]time.Time{}
	}
	w.lookedAt[[2]int{animeID, episode}] = time.Now()
}

// PreviewRow is what auto-download would do next for one show, and why.
type PreviewRow struct {
	AnimeID int    `json:"animeId"`
	Title   string `json:"title"`
	Episode int    `json:"episode,omitempty"`
	// download, wait, blocked, none (nothing found), have (on disk), queued, taken (found once, not on disk),
	// current (nothing new aired).
	State   string `json:"state"`
	Release string `json:"release,omitempty"`
	Size    int64  `json:"size,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// Preview says, show by show, what auto-download would take next and why, taking and recording nothing.
func (w *Watcher) Preview(ctx context.Context) ([]PreviewRow, error) {
	follows, err := w.store.Follows(ctx)
	if err != nil {
		return nil, err
	}
	queued, holds := map[[2]int]bool{}, map[[2]int]bool{}
	if rows, err := w.store.QueuedDownloads(ctx); err == nil {
		for _, q := range rows {
			queued[[2]int{q.AnimeID, q.Episode}] = true
		}
	}
	if waits, err := w.store.Waits(ctx, 0); err == nil {
		for _, h := range waits {
			holds[[2]int{h.AnimeID, h.Episode}] = true
		}
	}
	out := []PreviewRow{}
	var search []int
	for _, f := range follows {
		if !w.autoDownload(ctx, f.AnimeID) {
			continue
		}
		row := PreviewRow{AnimeID: f.AnimeID, Title: w.store.ShownTitle(ctx, f.AnimeID)}
		latest := f.Latest(time.Now().Unix())
		switch {
		case latest <= f.Progress || latest > f.Progress+maxBacklog:
			row.State = "current"
		case w.store.HeldEpisode(ctx, f.AnimeID, latest):
			row.State, row.Episode = "have", latest
		case queued[[2]int{f.AnimeID, latest}]:
			row.State, row.Episode = "queued", latest
		case !holds[[2]int{f.AnimeID, latest}] && w.store.EpisodeFetched(ctx, f.AnimeID, latest):
			// Found once already: the watch takes an episode one time, so it won't fetch this one again.
			row.State, row.Episode = "taken", latest
		default:
			row.Episode = latest
			search = append(search, len(out))
		}
		out = append(out, row)
	}
	// A few at a time: one by one, a long follow list keeps the page waiting; all at once, the sites limit.
	var wg sync.WaitGroup
	slots := make(chan struct{}, previewSearches)
	for _, i := range search {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			w.previewEpisode(ctx, &out[i])
		})
	}
	wg.Wait()
	return out, nil
}

// previewSearches is how many shows Preview searches at once.
const previewSearches = 3

func (w *Watcher) previewEpisode(ctx context.Context, row *PreviewRow) {
	prefs := w.prefsFor(ctx, row.AnimeID)
	ranked := w.find(ctx, row.AnimeID, row.Episode, prefs)
	best := firstConfirmed(ranked)
	switch {
	case best != nil && w.waitFor(ctx, row.AnimeID) > 0 && !score.Ideal(*best, prefs):
		row.State, row.Reason = "wait", fmt.Sprintf("waits up to %s for %s", plainHours(w.waitFor(ctx, row.AnimeID)), score.Wanted(prefs))
		row.Release, row.Size = best.Torrent.Title, best.EpisodeBytes()
	case best != nil:
		row.State, row.Release, row.Size = "download", best.Torrent.Title, best.EpisodeBytes()
	case len(ranked) > 0 && ranked[0].Blocked != "":
		row.State, row.Release, row.Reason = "blocked", ranked[0].Torrent.Title, ranked[0].Blocked
	case len(ranked) > 0:
		row.State, row.Reason = "none", "found only releases that don't name this episode"
	default:
		row.State, row.Reason = "none", "no release found yet"
	}
}

func plainHours(d time.Duration) string {
	if h := int(d.Hours()); h != 1 {
		return fmt.Sprintf("%d hours", h)
	}
	return "1 hour"
}

// Per-anime prefs overlay the global switch.
func (w *Watcher) autoDownload(ctx context.Context, animeID int) bool {
	prefs, err := w.store.Prefs(ctx, animeID)
	return err == nil && prefs.Bool("autodownload.enabled")
}

// Queued, so many shows updating at once don't start as many downloads, and behind manual ones. A pick is
// the release to take, over a copy already held; nil lets the queue choose.
func (w *Watcher) download(ctx context.Context, animeID, episode int, pick *score.Result) bool {
	if w.queue == nil {
		return false
	}
	// Season 0: the finder derives it from the title, as the manual path does.
	// Forcing 1 makes it reject every season-2 release it just announced.
	var err error
	if pick != nil {
		err = w.store.EnqueueRelease(ctx, animeID, 0, episode, pick.Torrent.InfoHash, pick.Torrent.Title)
	} else {
		_, err = w.store.Enqueue(ctx, animeID, 0, []int{episode})
	}
	if err != nil {
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
