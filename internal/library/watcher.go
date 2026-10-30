package library

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"yoru/internal/indexer"
	"yoru/internal/parse"
	"yoru/internal/score"
	"yoru/internal/store"
)

type Watcher struct {
	store   *store.Store
	finder  *Finder
	indexer indexer.Source
	queue   *Downloader
	log     *slog.Logger

	waitMu     sync.Mutex
	waiting    map[[2]int]time.Time
	lookedAt   map[[2]int]time.Time
	searchedAt map[[2]int]time.Time
}

func (w *Watcher) searched(animeID, episode int) {
	w.waitMu.Lock()
	defer w.waitMu.Unlock()
	if w.searchedAt == nil {
		w.searchedAt = map[[2]int]time.Time{}
	}
	w.searchedAt[[2]int{animeID, episode}] = time.Now()
}

func (w *Watcher) CheckedAt(animeID, episode int) (time.Time, bool) {
	w.waitMu.Lock()
	defer w.waitMu.Unlock()
	at, ok := w.searchedAt[[2]int{animeID, episode}]
	return at, ok
}

const searchedFor = 7 * 24 * time.Hour

func (w *Watcher) pruneSearched(follows []store.Follow) {
	followed := map[int]bool{}
	for _, f := range follows {
		followed[f.AnimeID] = true
	}
	w.waitMu.Lock()
	defer w.waitMu.Unlock()
	for key, at := range w.searchedAt {
		if !followed[key[0]] || time.Since(at) > searchedFor {
			delete(w.searchedAt, key)
		}
	}
}

type Coverage struct {
	Latest, Progress, Episodes, First, Wanted int
	Auto                                      bool
}

func (c Coverage) wantedOut() bool {
	return c.Wanted > 0 && c.Wanted <= c.Latest
}

func (c Coverage) inWindow() bool {
	return c.Latest > c.Progress && c.Latest <= c.Progress+maxBacklog && (c.Episodes <= 0 || c.Latest <= c.Episodes)
}

func (c Coverage) catchesUp(episode int) bool {
	return c.Auto && c.First > 0 && episode > max(c.Progress, c.First) && episode < c.Latest
}

func (c Coverage) Searches(episode int) bool {
	if c.Wanted > 0 && episode == c.Wanted {
		return c.wantedOut()
	}
	return c.inWindow() && (episode == c.Latest || c.catchesUp(episode))
}

func (w *Watcher) Coverage(ctx context.Context) map[int]Coverage {
	out := map[int]Coverage{}
	global, err := w.store.Prefs(ctx, 0)
	if err != nil {
		return out
	}
	rows, err := w.store.WatchRows(ctx)
	if err != nil {
		w.log.Warn("release watch coverage", "err", err)
		return out
	}
	flag := func(own, key string) bool {
		if own != "" {
			return own == "true"
		}
		return global.Bool(key)
	}
	now := time.Now().Unix()
	for _, r := range rows {
		notify, auto := flag(r.Notify, "notify.enabled"), flag(r.Auto, "autodownload.enabled")
		wanted, _ := strconv.Atoi(r.Want)
		if !notify && !auto && wanted <= 0 {
			continue
		}
		out[r.AnimeID] = Coverage{
			Latest: r.Latest(now), Progress: r.Progress, Episodes: r.Episodes, First: r.First, Wanted: wanted, Auto: auto,
		}
	}
	return out
}

var recheckWanted = 6 * time.Hour

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
	notifyOn := prefs.Bool("notify.enabled")
	want := audioFilter(prefs.String("notify.releases"))

	follows, err := w.store.Follows(ctx)
	if err != nil {
		return rep, err
	}
	w.announceDelays(ctx, follows, notifyOn, &rep)

	for _, f := range follows {
		auto := w.autoDownload(ctx, f.AnimeID)
		notifyOn, wanted := notifyOn, 0
		if p, err := w.store.Prefs(ctx, f.AnimeID); err == nil {
			notifyOn, wanted = p.Bool("notify.enabled"), p.Int("autodownload.want")
		}
		if !notifyOn && !auto && wanted == 0 {
			continue
		}
		rep.Checked++

		latest := f.Latest(time.Now().Unix())
		c := Coverage{Latest: latest, Progress: f.Progress, Episodes: f.Episodes, Wanted: wanted, Auto: auto}
		if auto {
			w.settle(ctx, f, latest, &rep)
		}
		if c.wantedOut() {
			w.fetchWanted(ctx, f.AnimeID, wanted, notifyOn, want, &rep)
		}
		if !c.inWindow() {
			continue
		}
		if auto {
			w.catchUp(ctx, f.AnimeID, c, want, &rep)
		}
		if seen, _ := w.store.AlreadyNotified(ctx, "", f.AnimeID, latest); seen || w.waitingFor(f.AnimeID, latest) {
			continue
		}
		w.check(ctx, f.AnimeID, latest, notifyOn, auto, want, &rep)
	}
	w.dropOrphanWaits(ctx, follows)
	w.pruneSearched(follows)
	return rep, nil
}

func (w *Watcher) fetchWanted(ctx context.Context, animeID, episode int, notifyOn bool, want func(parse.Release) bool, rep *WatchReport) {
	forget := func() { w.store.SetAnimePref(ctx, animeID, "autodownload.want", "") }
	switch {
	case w.store.HeldEpisode(ctx, animeID, episode):
		forget()
	case !w.store.EpisodeFetched(ctx, animeID, episode):
		w.check(ctx, animeID, episode, notifyOn, true, want, rep)
		if w.store.EpisodeFetched(ctx, animeID, episode) {
			forget()
		}
	default:
		if w.download(ctx, animeID, episode, nil) {
			rep.Downloaded++
			forget()
		}
	}
}

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

const maxBacklog = 200

const catchUpPerPoll = 4

func (w *Watcher) catchUp(ctx context.Context, animeID int, c Coverage, want func(parse.Release) bool, rep *WatchReport) {
	c.First = w.store.FirstRecorded(ctx, animeID)
	tried := 0
	for ep := max(c.Progress, c.First) + 1; c.catchesUp(ep) && tried < catchUpPerPoll; ep++ {
		if w.store.EpisodeFetched(ctx, animeID, ep) || w.waitingFor(animeID, ep) || w.store.HeldEpisode(ctx, animeID, ep) {
			continue
		}
		tried++
		w.check(ctx, animeID, ep, false, true, want, rep)
		if !w.store.EpisodeFetched(ctx, animeID, ep) {
			w.setWaiting(animeID, ep, true)
		}
	}
}

func (w *Watcher) check(ctx context.Context, animeID, episode int, notifyOn, auto bool, want func(parse.Release) bool, rep *WatchReport) {
	found, err := w.finder.Find(ctx, Request{
		AnimeID: animeID,
		Episode: episode,
		Prefs:   w.prefsFor(ctx, animeID),
	})
	if err != nil {
		w.log.Warn("watch poll", "anime", animeID, "episode", episode, "err", err)
		return
	}
	w.searched(animeID, episode)

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
	fetchedBefore := w.store.EpisodeFetched(ctx, animeID, episode)

	switch {
	case !notifyOn:
		if err := w.store.RecordNotified(ctx, fetch.Torrent.InfoHash, animeID, episode); err != nil {
			w.log.Warn("record release", "err", err)
		}
	case announce == nil:
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

	if auto && !fetchedBefore && !w.holding(ctx, animeID, episode, *fetch) && w.download(ctx, animeID, episode, nil) {
		rep.Downloaded++
	}
}

func (w *Watcher) asked(ctx context.Context, animeID int) int {
	p, err := w.store.Prefs(ctx, animeID)
	if err != nil {
		return 0
	}
	return p.Int("autodownload.want")
}

func (w *Watcher) waitFor(ctx context.Context, animeID int) time.Duration {
	p, err := w.store.Prefs(ctx, animeID)
	if err != nil {
		return 0
	}
	return time.Duration(max(0, p.Int64("autodownload.wait_hours"))) * time.Hour
}

func (w *Watcher) holding(ctx context.Context, animeID, episode int, best score.Result) bool {
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

const (
	lookAgainFor   = 48 * time.Hour
	lookAgainEvery = 6 * time.Hour
)

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

func (w *Watcher) find(ctx context.Context, animeID, episode int, prefs scorePrefs) []score.Result {
	found, err := w.finder.Find(ctx, Request{AnimeID: animeID, Episode: episode, Prefs: prefs})
	if err != nil {
		w.log.Warn("watch poll", "anime", animeID, "episode", episode, "err", err)
		return nil
	}
	w.searched(animeID, episode)
	return found.Results
}

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

type PreviewRow struct {
	AnimeID int    `json:"animeId"`
	Title   string `json:"title"`
	Episode int    `json:"episode,omitempty"`
	State   string `json:"state"`
	Release string `json:"release,omitempty"`
	Size    int64  `json:"size,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

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
			row.State, row.Episode = "taken", latest
		default:
			row.Episode = latest
			search = append(search, len(out))
		}
		out = append(out, row)
	}
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

func (w *Watcher) autoDownload(ctx context.Context, animeID int) bool {
	prefs, err := w.store.Prefs(ctx, animeID)
	return err == nil && prefs.Bool("autodownload.enabled")
}

func (w *Watcher) download(ctx context.Context, animeID, episode int, pick *score.Result) bool {
	if w.queue == nil {
		return false
	}
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

func audioFilter(pref string) func(parse.Release) bool {
	switch strings.ToLower(pref) {
	case "dub":
		return func(r parse.Release) bool {
			return r.DualAudio || mentionsDub(r)
		}
	case "either", "both":
		return func(parse.Release) bool { return true }
	default:
		return func(r parse.Release) bool {
			return r.DualAudio || !mentionsDub(r)
		}
	}
}

func mentionsDub(r parse.Release) bool {
	lower := strings.ToLower(r.Raw)
	return strings.Contains(lower, "dub") || strings.Contains(lower, "english audio")
}
