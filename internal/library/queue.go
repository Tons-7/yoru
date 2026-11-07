package library

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"yoru/internal/disk"
	"yoru/internal/score"
	"yoru/internal/store"
	"yoru/internal/torrent"
	"yoru/internal/transcode"
)

type Downloader struct {
	store    *store.Store
	prefetch *Prefetcher
	prefs    func(ctx context.Context, animeID int) score.Preferences
	log      *slog.Logger

	wake chan struct{}

	mu         sync.Mutex
	current    *inFlight
	userPaused map[string]bool
	watching   map[string]struct{}
	through    map[string]struct{}
	arrived    func(ctx context.Context, session string) bool
	paused     bool
	dir        string
	short      int64
	shortDrive string
}

const MinFree = 2 << 30

var freeSpace = disk.Free

func (d *Downloader) WithRoom(dir string) *Downloader { d.dir = dir; return d }

func (d *Downloader) Short() (free int64, drive string) {
	if d == nil {
		return 0, ""
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.short, d.shortDrive
}

func (d *Downloader) roomy() bool {
	if d.dir == "" {
		return true
	}
	free, drive, err := freeSpace(d.dir)
	if err != nil {
		return true
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	was := d.short
	if free >= MinFree {
		d.short = 0
		if was > 0 {
			d.log.Info("room on the downloads drive again; the queue carries on", "free", free)
		}
		return true
	}
	d.short, d.shortDrive = max(free, 1), drive
	if was == 0 {
		d.log.Warn("downloads drive nearly full; the queue waits for room", "free", free, "drive", drive)
	}
	return false
}

func (d *Downloader) WhenArrived(f func(ctx context.Context, session string) bool) {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.arrived = f
	d.mu.Unlock()
}

func (d *Downloader) Watching() bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return !d.paused && d.holding()
}

func (d *Downloader) holding() bool {
	for id := range d.watching {
		if _, past := d.through[id]; !past {
			return true
		}
	}
	return false
}

func (d *Downloader) Proceed() {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.through = map[string]struct{}{}
	for id := range d.watching {
		d.through[id] = struct{}{}
	}
	d.mu.Unlock()
	d.Wake()
}

func (d *Downloader) releaseArrived(ctx context.Context) {
	d.mu.Lock()
	arrived := d.arrived
	sessions := make([]string, 0, len(d.watching))
	for id := range d.watching {
		sessions = append(sessions, id)
	}
	d.mu.Unlock()
	if arrived == nil {
		return
	}
	for _, id := range sessions {
		if arrived(ctx, id) {
			d.log.Info("the episode playing has downloaded; the queue carries on", "session", id)
			d.Release(id)
		}
	}
}

const pausedSetting = "downloads.paused"

func (d *Downloader) Paused() bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.paused
}

func (d *Downloader) SetPaused(ctx context.Context, paused bool) error {
	if d == nil {
		return nil
	}
	if err := d.store.SetSetting(ctx, pausedSetting, fmt.Sprint(paused)); err != nil {
		return err
	}
	d.mu.Lock()
	d.paused = paused
	current := d.current
	d.mu.Unlock()

	if paused && current != nil {
		d.log.Info("download queue paused")
		current.stop()
	}
	if !paused {
		d.Wake()
	}
	return nil
}

func (d *Downloader) restorePaused(ctx context.Context) {
	if v, err := d.store.Setting(ctx, pausedSetting); err == nil && v == "true" {
		d.mu.Lock()
		d.paused = true
		d.mu.Unlock()
		d.log.Info("download queue is paused; resume it from Downloads")
	}
}

func (d *Downloader) Now(ctx context.Context, animeID int, epKey string) (bool, error) {
	if d == nil {
		return false, nil
	}
	moved, err := d.store.PrioritiseWaiting(ctx, animeID, epKey)
	if err != nil || !moved {
		return moved, err
	}
	if rec, ok, err := d.store.TorrentForEpisode(ctx, animeID, epKey); err == nil && ok && d.prefetch != nil {
		d.prefetch.Unpause(ctx, rec)
		d.markUserPaused(rec.InfoHash, false)
		d.markUserPaused(FileKey(rec.InfoHash, rec.FileIndex), false)
	}
	d.mu.Lock()
	current := d.current
	d.mu.Unlock()
	if current != nil && (current.animeID != animeID || current.epKey != epKey) {
		current.stop()
	}
	return true, d.SetPaused(ctx, false)
}

func (d *Downloader) Restart(ctx context.Context, animeID int, epKey string) (bool, error) {
	if d == nil {
		return false, nil
	}
	moved, err := d.store.Promote(ctx, animeID, epKey)
	if err != nil || !moved {
		return moved, err
	}
	d.mu.Lock()
	current := d.current
	d.mu.Unlock()
	if current != nil {
		current.stop()
	}
	return true, d.SetPaused(ctx, false)
}

type inFlight struct {
	animeID int
	epKey   string
	stop    context.CancelFunc
}

func (f *inFlight) session() string {
	return fmt.Sprintf("%d-%s", f.animeID, f.epKey)
}

func NewDownloader(s *store.Store, p *Prefetcher, prefs func(context.Context, int) score.Preferences, log *slog.Logger) *Downloader {
	return &Downloader{
		store:    s,
		prefetch: p,
		prefs:    prefs,
		log:      log,
		wake:     make(chan struct{}, 1),
		watching: map[string]struct{}{},
	}
}

func (d *Downloader) Hold(sessionID string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	_, already := d.watching[sessionID]
	_, through := d.through[sessionID]
	d.watching[sessionID] = struct{}{}
	current := d.current
	d.mu.Unlock()

	if already || current == nil || through {
		return
	}

	if current.session() == transcode.BaseID(sessionID) {
		return
	}

	d.log.Info("pausing the queue while an episode plays", "session", sessionID)
	current.stop()
}

func (d *Downloader) Downloading(sessionID string) bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.current != nil && d.current.session() == transcode.BaseID(sessionID)
}

func (d *Downloader) Release(sessionID string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	delete(d.watching, sessionID)
	delete(d.through, sessionID)
	free := !d.holding()
	d.mu.Unlock()

	if free {
		d.Wake()
	}
}

func (d *Downloader) Held() bool { return d.held() }

func (d *Downloader) held() bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.paused || d.holding()
}

func (d *Downloader) Wake() {
	if d == nil {
		return
	}
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

var (
	queueIdle   = 30 * time.Second
	watchedIdle = 5 * time.Second

	queueStall = 10 * time.Minute

	queueRetries    = 2
	queueRetryDelay = 5 * time.Minute
)

var (
	quietRecheck = 10 * time.Second
	quietFor     = 15 * time.Minute
)

func (d *Downloader) Quiet(ctx context.Context) {
	if d == nil || d.prefetch == nil || d.prefetch.torrent == nil {
		return
	}
	d.restoreUserPaused(ctx)
	pending := d.quiet(ctx, nil)
	if len(pending) == 0 {
		return
	}
	recheck := quietRecheck
	go func() {
		deadline := time.Now().Add(quietFor)
		for time.Now().Before(deadline) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(recheck):
			}
			if pending = d.quiet(ctx, pending); len(pending) == 0 {
				return
			}
		}
	}()
}

func (d *Downloader) quiet(ctx context.Context, only map[string]bool) map[string]bool {
	var started map[string]bool
	if prefs, err := d.store.Prefs(ctx, 0); err == nil && prefs.Bool("cache.prefetch_full") {
		if started, err = d.store.StartedTorrents(ctx); err != nil {
			d.log.Warn("list started downloads", "err", err)
		}
	}
	pinned := map[string]bool{}
	if entries, err := d.store.CacheEntries(ctx); err == nil {
		for _, e := range entries {
			if e.Pinned {
				pinned[strings.ToLower(e.InfoHash)] = true
			}
		}
	}
	keep := func(hash string) bool {
		if d.userPausedHash(hash) && !pinned[hash] {
			return false
		}
		return started[hash] || pinned[hash] || d.prefetch.Claimed(hash) || (only != nil && !only[hash])
	}

	paused, kept, checking, err := d.prefetch.torrent.PauseUnfinished(ctx, keep)
	if err != nil {
		d.log.Warn("quiet the torrent engine", "err", err)
		return nil
	}
	if paused > 0 || len(checking) > 0 {
		d.log.Info("part-downloaded torrents on startup",
			"paused", paused, "leftDownloading", kept, "stillChecking", len(checking))
	}
	still := map[string]bool{}
	for _, hash := range checking {
		if only == nil || only[hash] {
			still[hash] = true
		}
	}
	return still
}

func (d *Downloader) Run(ctx context.Context) {
	if d == nil || d.prefetch == nil {
		return
	}

	if err := d.store.ResetActive(ctx); err != nil {
		d.log.Warn("reset download queue", "err", err)
	}
	d.restorePaused(ctx)
	d.restoreUserPaused(ctx)

	for {
		d.releaseArrived(ctx)
		worked := !d.held() && d.roomy() && d.step(ctx)
		if ctx.Err() != nil {
			return
		}
		if worked {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-d.wake:
		case <-time.After(d.idle(ctx)):
		}
	}
}

func (d *Downloader) idle(ctx context.Context) time.Duration {
	wait := queueIdle
	if d.Watching() {
		wait = watchedIdle
	}
	at, ok, err := d.store.NextRetry(ctx)
	if err != nil || !ok {
		return wait
	}
	return max(time.Millisecond, min(wait, time.Until(at)))
}

func swarmFailure(err error) bool {
	var stall *torrent.StallError
	return errors.Is(err, ErrSeedersSilent) || errors.As(err, &stall)
}

func describe(err error) error {
	var stall *torrent.StallError
	switch {
	case errors.As(err, &stall) && stall.Cause != "":
		return fmt.Errorf("%s; %.0f%% is on disk", stall.Cause, stall.Percent)
	case errors.As(err, &stall):
		minutes := max(1, int(stall.Quiet.Round(time.Minute).Minutes()))
		return fmt.Errorf("no peers sent anything for %d minute%s; %.0f%% is on disk",
			minutes, plural(minutes), stall.Percent)
	case errors.Is(err, ErrSeedersSilent):
		return ErrSeedersSilent
	}
	return err
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func (d *Downloader) Cancel(ctx context.Context, animeID int, epKey string) (int, error) {
	if d == nil {
		return 0, nil
	}

	d.mu.Lock()
	c := d.current
	d.mu.Unlock()
	matches := c != nil && c.animeID == animeID && (epKey == "" || c.epKey == epKey)
	hasRow := false
	if matches {
		if rows, err := d.store.QueuedDownloads(ctx); err == nil {
			for _, r := range rows {
				hasRow = hasRow || (r.AnimeID == c.animeID && r.EpKey == c.epKey)
			}
		}
	}

	removed, err := d.store.Dequeue(ctx, animeID, epKey)
	if err != nil {
		return 0, err
	}

	d.mu.Lock()
	if c := d.current; c != nil && c.animeID == animeID && (epKey == "" || c.epKey == epKey) {
		c.stop()
		if !hasRow {
			removed++
		}
	}
	d.mu.Unlock()

	return removed, nil
}

func (d *Downloader) SetUserPaused(hash string, paused bool) {
	if d == nil {
		return
	}
	d.markUserPaused(hash, paused)
	if err := d.store.SetHandPaused(context.Background(), hash, paused); err != nil {
		d.log.Warn("remember a download paused by hand", "err", err)
	}
	if !paused {
		d.Wake()
	}
}

func (d *Downloader) PauseEpisode(ctx context.Context, animeID int, epKey string, paused bool) (bool, error) {
	if d == nil {
		return false, nil
	}
	changed, err := d.store.SetQueuedPaused(ctx, animeID, epKey, paused)
	if err != nil {
		return false, err
	}
	if paused {
		d.StopEpisode(animeID, epKey)
	} else {
		d.Wake()
	}
	return changed, nil
}

func (d *Downloader) Move(ctx context.Context, animeID int, epKey, to string) (bool, error) {
	if d == nil {
		return false, nil
	}
	moved, err := d.store.MoveQueued(ctx, animeID, epKey, to)
	if moved {
		d.Wake()
	}
	return moved, err
}

func (d *Downloader) MoveNear(ctx context.Context, animeID int, epKey string, targetAnime int, targetEp string, after bool) (bool, error) {
	if d == nil {
		return false, nil
	}
	moved, err := d.store.MoveQueuedNear(ctx, animeID, epKey, targetAnime, targetEp, after)
	if moved {
		d.Wake()
	}
	return moved, err
}

func (d *Downloader) StopEpisode(animeID int, epKey string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	c := d.current
	d.mu.Unlock()
	if c != nil && c.animeID == animeID && c.epKey == epKey {
		c.stop()
	}
}

func (d *Downloader) markUserPaused(hash string, paused bool) {
	d.mu.Lock()
	if d.userPaused == nil {
		d.userPaused = map[string]bool{}
	}
	if paused {
		d.userPaused[strings.ToLower(hash)] = true
	} else {
		delete(d.userPaused, strings.ToLower(hash))
	}
	d.mu.Unlock()
	if d.prefetch != nil {
		d.prefetch.SetUserPaused(hash, paused)
	}
}

func (d *Downloader) restoreUserPaused(ctx context.Context) {
	d.PruneHandPaused(ctx)
	hashes, err := d.store.HandPaused(ctx)
	if err != nil {
		d.log.Warn("read downloads paused by hand", "err", err)
		return
	}
	for _, h := range hashes {
		d.markUserPaused(h, true)
	}
}

func (d *Downloader) PruneHandPaused(ctx context.Context) {
	if d == nil {
		return
	}
	keys, err := d.store.HandPaused(ctx)
	if err != nil || len(keys) == 0 {
		return
	}
	known, err := d.store.KnownTorrents(ctx)
	if err != nil {
		return
	}
	for _, k := range keys {
		hash, _, _ := strings.Cut(k, "/")
		if !known[hash] {
			d.forgetPause(ctx, k)
		}
	}
}

func (d *Downloader) forgetPause(ctx context.Context, key string) {
	d.markUserPaused(key, false)
	if err := d.store.SetHandPaused(ctx, key, false); err != nil {
		d.log.Warn("forget a pause by hand", "err", err)
	}
}

func (d *Downloader) Forget(ctx context.Context, hash string, fileIndex int, episodes []store.EpisodeRef) {
	if d == nil {
		return
	}
	for _, e := range episodes {
		if _, err := d.Cancel(ctx, e.AnimeID, e.EpKey); err != nil {
			d.log.Warn("drop a removed episode from the queue", "err", err)
		}
	}
	if fileIndex >= 0 {
		d.forgetPause(ctx, FileKey(hash, fileIndex))
		return
	}
	keys, _ := d.store.HandPaused(ctx)
	for _, k := range keys {
		if k == strings.ToLower(hash) || strings.HasPrefix(k, strings.ToLower(hash)+"/") {
			d.forgetPause(ctx, k)
		}
	}
}

func (d *Downloader) PausedByHand(key string) bool {
	if d == nil {
		return false
	}
	return d.userPausedHash(key)
}

func (d *Downloader) userPausedHash(hash string) bool {
	if d.prefetch != nil {
		return d.prefetch.pausedByUser(hash)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.userPaused[strings.ToLower(hash)]
}

func (d *Downloader) pausedByUser(ctx context.Context, animeID int, ep string) bool {
	rec, ok, err := d.store.TorrentForEpisode(ctx, animeID, ep)
	if err != nil || !ok {
		return false
	}
	return d.userPausedHash(rec.InfoHash)
}

func (d *Downloader) step(parent context.Context) bool {
	next, ok, err := d.store.NextQueued(parent)
	if err != nil {
		d.log.Warn("read download queue", "err", err)
		return false
	}
	if !ok {
		return false
	}

	d.log.Info("downloading queued episode",
		"anime", next.AnimeID, "episode", next.Episode)

	if after, ok, err := d.store.PeekQueued(parent); err == nil && ok && after.InfoHash == "" {
		d.prefetch.Ahead(after.AnimeID, after.Episode, after.Season, d.prefs(parent, after.AnimeID))
	}

	ctx, stop := context.WithCancel(parent)
	defer stop()

	d.mu.Lock()
	d.current = &inFlight{animeID: next.AnimeID, epKey: next.EpKey, stop: stop}
	if d.paused || d.holding() {
		stop()
	}
	d.mu.Unlock()

	err = d.prefetch.DownloadRelease(ctx, next.AnimeID, next.Episode, next.Season, d.prefs(parent, next.AnimeID), queueStall, next.InfoHash)

	d.mu.Lock()
	d.current = nil
	d.mu.Unlock()

	switch {
	case ctx.Err() != nil && parent.Err() == nil:
		requeued, err := d.store.RequeueActive(parent, next.AnimeID, next.EpKey)
		if err != nil {
			d.log.Warn("requeue held download", "err", err)
		}
		if requeued {
			d.prefetch.standDown(parent, next.AnimeID, next.Episode)
			d.log.Info("queue stood down, episode back in line",
				"anime", next.AnimeID, "episode", next.Episode)
		} else {
			d.log.Info("queued download cancelled",
				"anime", next.AnimeID, "episode", next.Episode)
		}
		return true

	case errors.Is(err, errReleasePaused), errors.Is(err, errEpisodePaused):
		d.log.Info("queued episode stays paused in its place: it was paused from the downloads list",
			"anime", next.AnimeID, "episode", next.Episode)
		if _, err := d.store.SetQueuedPaused(parent, next.AnimeID, next.EpKey, true); err != nil {
			d.log.Warn("keep a paused episode in line", "err", err)
		}
		return true

	case errors.Is(err, torrent.ErrPaused):
		if !d.pausedByUser(parent, next.AnimeID, next.EpKey) {
			if _, err := d.store.RequeueActive(parent, next.AnimeID, next.EpKey); err != nil {
				d.log.Warn("requeue paused download", "err", err)
			}
			return true
		}
		d.log.Info("queued download paused; it keeps its place in line",
			"anime", next.AnimeID, "episode", next.Episode)
		if _, err := d.store.SetQueuedPaused(parent, next.AnimeID, next.EpKey, true); err != nil {
			d.log.Warn("keep a paused episode in line", "err", err)
		}
		return true

	case err != nil && swarmFailure(err) && next.Attempts < queueRetries:
		wait := queueRetryDelay * time.Duration(next.Attempts+1)
		d.log.Info("the swarm gave nothing; trying again later",
			"anime", next.AnimeID, "episode", next.Episode, "in", wait, "reason", err)
		if err := d.store.Defer(parent, next.AnimeID, next.EpKey, describe(err), time.Now().Add(wait)); err != nil {
			d.log.Warn("defer queued download", "err", err)
		}
		return true

	case err != nil:
		d.log.Warn("queued download failed",
			"anime", next.AnimeID, "episode", next.Episode, "err", err)
		err = describe(err)
		if next.Attempts > 0 {
			err = fmt.Errorf("gave up after %d tries: %w", next.Attempts+1, err)
		}
	}

	if err := d.store.FinishQueued(parent, next.AnimeID, next.EpKey, err); err != nil {
		d.log.Warn("finish queued download", "err", err)
	}
	return true
}
