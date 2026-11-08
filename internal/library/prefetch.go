package library

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"yoru/internal/indexer"
	"yoru/internal/parse"
	"yoru/internal/score"
	"yoru/internal/store"
	"yoru/internal/torrent"
)

const (
	prepareRetry = 10 * time.Minute

	prepareWait = 45 * time.Second
)

type Prefetcher struct {
	store   *store.Store
	finder  *Finder
	torrent *torrent.Client
	log     *slog.Logger

	mu sync.Mutex

	running map[string]chan struct{}

	attempted map[string]time.Time

	prepared map[string]preparedRelease

	claimed map[string]int

	fetching map[string]*inFlightFetch

	room func(ctx context.Context, need int64, animeID int) bool

	slow       map[string]time.Time
	switched   map[string][]time.Time
	userPaused map[string]bool

	onSlow func(animeID, episode int)
}

func (p *Prefetcher) WhenSlow(f func(animeID, episode int)) {
	p.mu.Lock()
	p.onSlow = f
	p.mu.Unlock()
}

func (p *Prefetcher) reportSlow(animeID, episode int) {
	p.mu.Lock()
	f := p.onSlow
	p.mu.Unlock()
	if f != nil {
		f(animeID, episode)
	}
}

var (
	errReleasePaused = errors.New("its release is paused; resume it, or change the release")
	errEpisodePaused = errors.New("this episode is paused; resume it to download")
)

func FileKey(hash string, index int) string {
	return fmt.Sprintf("%s/%d", strings.ToLower(hash), index)
}

func (p *Prefetcher) SetUserPaused(hash string, paused bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.userPaused == nil {
		p.userPaused = map[string]bool{}
	}
	if paused {
		p.userPaused[strings.ToLower(hash)] = true
	} else {
		delete(p.userPaused, strings.ToLower(hash))
	}
}

func (p *Prefetcher) pausedByUser(hash string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.userPaused[strings.ToLower(hash)]
}

func (p *Prefetcher) narrowPause(ctx context.Context, hash string, except int) {
	hash = strings.ToLower(hash)
	if !p.pausedByUser(hash) {
		return
	}
	var id int
	var held bool
	if p.torrent != nil {
		live, _ := p.torrent.Live(ctx)
		id, held = live[hash]
	}
	parts, _ := p.store.DownloadParts(ctx)
	for _, part := range parts[hash] {
		if part.FileIndex == except || part.Complete {
			continue
		}
		if held {
			if err := p.torrent.IncludeFile(ctx, id, part.FileIndex, false); err != nil {
				p.log.Debug("keep the rest of a paused pack paused", "torrent", id, "err", err)
			}
		}
		k := FileKey(hash, part.FileIndex)
		p.SetUserPaused(k, true)
		if err := p.store.SetHandPaused(ctx, k, true); err != nil {
			p.log.Warn("remember a pause by hand", "err", err)
		}
	}
}

func (p *Prefetcher) Unpause(ctx context.Context, rec store.TorrentRecord) {
	keys := []string{strings.ToLower(rec.InfoHash), FileKey(rec.InfoHash, rec.FileIndex)}
	if p.torrent != nil {
		p.narrowPause(ctx, keys[0], rec.FileIndex)
	}
	var was []string
	for _, k := range keys {
		if p.pausedByUser(k) {
			was = append(was, k)
			p.SetUserPaused(k, false)
			if err := p.store.SetHandPaused(ctx, k, false); err != nil {
				p.log.Warn("forget a pause by hand", "err", err)
			}
		}
	}
	if len(was) == 0 || p.torrent == nil {
		return
	}
	live, err := p.torrent.Live(ctx)
	if err != nil {
		return
	}
	if id, ok := live[keys[0]]; ok {
		if rec.FileIndex >= 0 {
			p.torrent.IncludeFile(ctx, id, rec.FileIndex, true)
		}
		p.torrent.Start(ctx, id)
	}
	p.log.Info("a download paused by hand was asked for again", "anime", rec.AnimeID, "episode", rec.EpKey)
}

func (p *Prefetcher) standDown(ctx context.Context, animeID, episode int) {
	rec, ok := p.held(ctx, animeID, episode)
	if !ok || rec.FileIndex < 0 || p.store.TorrentEpisodeCount(ctx, rec.InfoHash) < 2 ||
		p.store.FileComplete(ctx, rec.InfoHash, rec.FileIndex) {
		return
	}
	if playing, err := p.pinned(ctx, rec.InfoHash); err != nil || playing {
		return
	}
	live, err := p.torrent.Live(ctx)
	if err != nil {
		return
	}
	if id, ok := live[strings.ToLower(rec.InfoHash)]; ok {
		if err := p.torrent.IncludeFile(ctx, id, rec.FileIndex, false); err != nil {
			p.log.Debug("stand an episode of a pack down", "torrent", id, "err", err)
		}
	}
}

type inFlightFetch struct {
	added chan struct{}
	done  chan struct{}
	once  sync.Once
	rec   store.TorrentRecord

	hash      string
	taken     bool
	abandoned bool
}

const (
	healthySwarm = 10
	slowSwitches = 2
	slowForget   = 6 * time.Hour
	slowWindow   = 3 * time.Minute
	slowShare    = 0.25
)

func (p *Prefetcher) ruleOut(animeID, episode int, hash string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	if p.slow == nil {
		p.slow, p.switched = map[string]time.Time{}, map[string][]time.Time{}
	}
	for h, until := range p.slow {
		if !now.Before(until) {
			delete(p.slow, h)
		}
	}
	for k, times := range p.switched {
		if times = slices.DeleteFunc(times, func(at time.Time) bool { return now.Sub(at) >= slowForget }); len(times) == 0 {
			delete(p.switched, k)
		} else {
			p.switched[k] = times
		}
	}
	p.slow[strings.ToLower(hash)] = now.Add(slowForget)
	key := fetchKey(animeID, episode)
	p.switched[key] = append(p.switched[key], now)
	return len(p.switched[key])
}

func (p *Prefetcher) ruledOut(hash string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	until, ok := p.slow[strings.ToLower(hash)]
	return ok && time.Now().Before(until)
}

func (p *Prefetcher) switches(animeID, episode int) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, at := range p.switched[fetchKey(animeID, episode)] {
		if time.Since(at) < slowForget {
			n++
		}
	}
	return n
}

var errOvertaken = errors.New("episode fetched by another path")

func fetchKey(animeID, episode int) string { return fmt.Sprintf("fetch:%d/%d", animeID, episode) }

func (p *Prefetcher) beginFetch(animeID, episode int) (*inFlightFetch, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fetching == nil {
		p.fetching = map[string]*inFlightFetch{}
	}
	key := fetchKey(animeID, episode)
	if _, busy := p.fetching[key]; busy {
		return nil, false
	}
	f := &inFlightFetch{added: make(chan struct{}), done: make(chan struct{})}
	p.fetching[key] = f
	return f, true
}

func (p *Prefetcher) endFetch(animeID, episode int, f *inFlightFetch) {
	p.mu.Lock()
	delete(p.fetching, fetchKey(animeID, episode))
	p.unclaimLocked(f.hash)
	p.mu.Unlock()
	f.once.Do(func() { close(f.added) })
	close(f.done)
}

func (p *Prefetcher) setTorrent(f *inFlightFetch, rec store.TorrentRecord) {
	p.mu.Lock()
	f.rec = rec
	p.mu.Unlock()
	if rec.InfoHash != "" {
		f.once.Do(func() { close(f.added) })
	}
}

func (p *Prefetcher) Fetching(animeID, episode int) (store.TorrentRecord, bool) {
	if p == nil {
		return store.TorrentRecord{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	f := p.fetching[fetchKey(animeID, episode)]
	if f == nil || f.rec.InfoHash == "" {
		return store.TorrentRecord{}, false
	}
	f.taken = true
	return f.rec, true
}

func (p *Prefetcher) takenOver(f *inFlightFetch) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if f.taken {
		f.abandoned = true
	}
	return f.taken
}

func (p *Prefetcher) HandBack(animeID, episode int, hash string) bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	f := p.fetching[fetchKey(animeID, episode)]
	if f == nil || f.abandoned || !strings.EqualFold(f.rec.InfoHash, hash) {
		return false
	}
	f.taken = false
	return true
}

func (p *Prefetcher) inFlight(animeID, episode int) *inFlightFetch {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fetching[fetchKey(animeID, episode)]
}

func (p *Prefetcher) WithRoom(room func(ctx context.Context, need int64, animeID int) bool) *Prefetcher {
	p.room = room
	return p
}

func (p *Prefetcher) claim(f *inFlightFetch, hash string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if f.hash != "" {
		p.unclaimLocked(f.hash)
	}
	f.hash = strings.ToLower(hash)
	if f.hash != "" {
		p.claimLocked(f.hash)
	}
}

func (p *Prefetcher) claimLocked(hash string) {
	if p.claimed == nil {
		p.claimed = map[string]int{}
	}
	p.claimed[hash]++
}

func (p *Prefetcher) unclaimLocked(hash string) {
	if hash == "" {
		return
	}
	if p.claimed[hash]--; p.claimed[hash] <= 0 {
		delete(p.claimed, hash)
	}
}

func (p *Prefetcher) claimWhile(hash string) func() {
	hash = strings.ToLower(hash)
	p.mu.Lock()
	p.claimLocked(hash)
	p.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			p.mu.Lock()
			p.unclaimLocked(hash)
			p.mu.Unlock()
		})
	}
}

func (p *Prefetcher) discardIfAlone(ctx context.Context, id int, hash string) {
	if p.claims(hash) > 1 {
		p.log.Info("leaving a release this episode gave up: another episode downloads from it", "torrent", id)
		return
	}
	discardUnclaimed(ctx, p.store, p.torrent, p.log, id, hash)
}

func (p *Prefetcher) Claimed(hash string) bool {
	return p.claims(hash) > 0
}

func (p *Prefetcher) claims(hash string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.claimed[strings.ToLower(hash)]
}

var preparedFresh = 30 * time.Minute

type preparedRelease struct {
	result score.Result
	at     time.Time
}

func NewPrefetcher(s *store.Store, f *Finder, tc *torrent.Client, log *slog.Logger) *Prefetcher {
	return &Prefetcher{
		store: s, finder: f, torrent: tc, log: log,
		running:   map[string]chan struct{}{},
		attempted: map[string]time.Time{},
		prepared:  map[string]preparedRelease{},
	}
}

func (p *Prefetcher) prepareWanted() bool {
	global, err := p.store.Prefs(context.Background(), 0)
	return err == nil && global.Bool("playback.prepare_next")
}

func prepareKey(animeID, episode int) string {
	return fmt.Sprintf("prepare:%d/%d", animeID, episode)
}

func (p *Prefetcher) ahead() int {
	global, err := p.store.Prefs(context.Background(), 0)
	if err != nil || !global.Bool("cache.prefetch_next") {
		return 0
	}
	return min(max(global.Int("cache.prefetch_count"), 1), 2)
}

func (p *Prefetcher) Next(animeID, episode, season int, prefs score.Preferences) {
	if p == nil || p.torrent == nil || animeID == 0 || episode <= 0 {
		return
	}
	count := p.ahead()
	if count == 0 {
		return
	}

	key := fmt.Sprintf("%d/%d", animeID, episode)
	p.mu.Lock()
	if _, busy := p.running[key]; busy {
		p.mu.Unlock()
		return
	}
	done := make(chan struct{})
	p.running[key] = done
	p.mu.Unlock()

	go func() {
		defer func() {
			p.mu.Lock()
			delete(p.running, key)
			p.mu.Unlock()
			close(done)
		}()

		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(count)*30*time.Minute)
		defer cancel()

		picks, _, err := p.store.PlayOrder(ctx, animeID, episode, count)
		if err != nil {
			return
		}
		for i, next := range picks {
			nextPrefs, nextSeason := p.prefsOf(ctx, next.AnimeID, animeID, prefs, season)
			id, started, err := p.fetch(ctx, next.AnimeID, next.Episode, nextSeason, nextPrefs, false)
			if err != nil {
				if !errors.Is(err, errOvertaken) {
					p.log.Info("next episode not downloaded ahead", "anime", next.AnimeID, "episode", next.Episode, "err", err)
				}
				return
			}
			if started && i < len(picks)-1 {
				if err := p.torrent.Await(ctx, id, prefetchStall); err != nil {
					p.log.Debug("prefetch stopped waiting", "anime", next.AnimeID, "episode", next.Episode, "err", err)
					return
				}
			}
		}
	}()
}

const prefetchStall = 3 * time.Minute

func (p *Prefetcher) Prepare(animeID, episode, season int, prefs score.Preferences) {
	if p == nil {
		return
	}
	ctx := context.Background()
	picks, _, err := p.store.PlayOrder(ctx, animeID, episode, 1)
	if err != nil || len(picks) == 0 {
		return
	}
	next := picks[0]
	prefs, season = p.prefsOf(ctx, next.AnimeID, animeID, prefs, season)
	p.PrepareTarget(next.AnimeID, next.Episode, season, prefs)
}

func (p *Prefetcher) prefsOf(ctx context.Context, animeID, playing int, prefs score.Preferences, season int) (score.Preferences, int) {
	if animeID == playing {
		return prefs, season
	}
	resolved, err := p.store.Prefs(ctx, animeID)
	if err != nil {
		return score.DefaultPreferences(), 0
	}
	return Preferences(resolved), 0
}

func (p *Prefetcher) PrepareTarget(animeID, episode, season int, prefs score.Preferences) {
	if p == nil || p.torrent == nil || animeID == 0 || episode <= 0 {
		return
	}
	if !p.prepareWanted() {
		return
	}
	p.resolveAsync(animeID, episode, season, prefs)
}

func (p *Prefetcher) Ahead(animeID, episode, season int, prefs score.Preferences) {
	if p == nil || p.torrent == nil || animeID == 0 || episode <= 0 {
		return
	}
	p.resolveAsync(animeID, episode, season, prefs)
}

func (p *Prefetcher) resolveAsync(animeID, episode, season int, prefs score.Preferences) {
	key := prepareKey(animeID, episode)

	p.mu.Lock()
	if _, busy := p.running[key]; busy {
		p.mu.Unlock()
		return
	}
	if at, tried := p.attempted[key]; tried && time.Since(at) < prepareRetry {
		p.mu.Unlock()
		return
	}
	if got, ok := p.prepared[key]; ok && time.Since(got.at) < preparedFresh {
		p.mu.Unlock()
		return
	}
	done := make(chan struct{})
	p.running[key] = done
	p.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		err := p.prepare(ctx, animeID, episode, season, prefs)

		p.mu.Lock()
		delete(p.running, key)
		if err != nil {
			for k, at := range p.attempted {
				if time.Since(at) >= prepareRetry {
					delete(p.attempted, k)
				}
			}
			p.attempted[key] = time.Now()
		} else {
			delete(p.attempted, key)
		}
		p.mu.Unlock()
		close(done)

		if err != nil {
			p.log.Debug("prepare skipped", "anime", animeID, "episode", episode, "err", err)
		}
	}()
}

func (p *Prefetcher) AwaitPrepare(ctx context.Context, animeID, episode int) {
	if p == nil {
		return
	}
	deadline := time.After(prepareWait)
	p.awaitResolve(ctx, animeID, episode, deadline)

	p.mu.Lock()
	f := p.fetching[fetchKey(animeID, episode)]
	p.mu.Unlock()
	if f == nil {
		return
	}
	p.log.Info("waiting for the download already started for it", "anime", animeID, "episode", episode)
	select {
	case <-f.added:
	case <-ctx.Done():
	case <-deadline:
	}
}

func (p *Prefetcher) awaitResolve(ctx context.Context, animeID, episode int, deadline <-chan time.Time) {
	p.mu.Lock()
	done := p.running[prepareKey(animeID, episode)]
	p.mu.Unlock()
	if done == nil {
		return
	}

	p.log.Info("waiting for the release already being made ready",
		"anime", animeID, "episode", episode)
	select {
	case <-done:
	case <-ctx.Done():
	case <-deadline:
	}
}

func (p *Prefetcher) held(ctx context.Context, animeID, episode int) (store.TorrentRecord, bool) {
	rec, ok, err := p.store.TorrentForEpisode(ctx, animeID, epKey(episode))
	if err != nil || !ok || !heldStands(ctx, p.store, p.finder, animeID, episode, rec) {
		return store.TorrentRecord{}, false
	}
	return rec, true
}

func (p *Prefetcher) prepare(ctx context.Context, animeID, episode, season int, prefs score.Preferences) error {
	if _, ok := p.held(ctx, animeID, episode); ok || p.inFlight(animeID, episode) != nil {
		return nil
	}

	if _, ok := localCopy(ctx, p.store, p.log, animeID, episode); ok {
		return nil
	}

	total, _ := p.store.EpisodeCount(ctx, animeID)
	if total > 0 && episode > total {
		return nil
	}

	found, err := p.finder.Find(ctx, Request{
		AnimeID: animeID, Episode: episode, Season: season, Prefs: prefs,
	})
	if err != nil {
		return err
	}
	if found.Best == nil {
		return fmt.Errorf("no release found")
	}

	p.storePrepared(animeID, episode, *found.Best)
	p.log.Info("next episode resolved", "anime", animeID, "episode", episode, "group", found.Best.Release.Group)
	return nil
}

func (p *Prefetcher) storePrepared(animeID, episode int, r score.Result) {
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, v := range p.prepared {
		if now.Sub(v.at) > preparedFresh {
			delete(p.prepared, k)
		}
	}
	p.prepared[prepareKey(animeID, episode)] = preparedRelease{result: r, at: now}
}

func (p *Prefetcher) takePreparedUnless(animeID, episode int, picked bool) (score.Result, bool) {
	if picked {
		return score.Result{}, false
	}
	return p.TakePrepared(animeID, episode)
}

func (p *Prefetcher) TakePrepared(animeID, episode int) (score.Result, bool) {
	if p == nil {
		return score.Result{}, false
	}
	key := prepareKey(animeID, episode)
	p.mu.Lock()
	defer p.mu.Unlock()
	got, ok := p.prepared[key]
	if !ok {
		return score.Result{}, false
	}
	delete(p.prepared, key)
	if time.Since(got.at) > preparedFresh {
		return score.Result{}, false
	}
	return got.result, true
}

func (p *Prefetcher) Download(ctx context.Context, animeID, episode, season int, prefs score.Preferences, stall time.Duration) error {
	return p.DownloadRelease(ctx, animeID, episode, season, prefs, stall, "")
}

func (p *Prefetcher) DownloadRelease(ctx context.Context, animeID, episode, season int, prefs score.Preferences, stall time.Duration, pick string) error {
	if p == nil || p.torrent == nil {
		return fmt.Errorf("torrent engine unavailable")
	}
	for {
		err := p.download(ctx, animeID, episode, season, prefs, stall, pick)
		if !errors.Is(err, errOvertaken) {
			return err
		}
		if f := p.inFlight(animeID, episode); f != nil {
			select {
			case <-f.done:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

func (p *Prefetcher) download(ctx context.Context, animeID, episode, season int, prefs score.Preferences, stall time.Duration, pick string) error {
	switching := pick == ""
	var carried time.Duration
	last := int64(-1)
	for {
		err := p.downloadOnce(ctx, animeID, episode, season, prefs, max(stall-carried, torrent.AwaitPoll), pick, true)
		var slow *torrent.SlowError
		if !errors.As(err, &slow) {
			return err
		}
		if slow.Bytes == last {
			carried += slow.Quiet
		} else {
			carried = slow.Quiet
		}
		last = slow.Bytes
		if switching && p.replaceable(ctx, animeID, episode) && p.switchAway(ctx, animeID, episode, season, prefs, slow) {
			carried, last = 0, -1
			continue
		}
		p.reportSlow(animeID, episode)
		switching = false
		if ctx.Err() != nil {
			p.stop(context.WithoutCancel(ctx), slow.ID, false, "was stood down while slow", ctx.Err())
			return ctx.Err()
		}
	}
}

func (p *Prefetcher) replaceable(ctx context.Context, animeID, episode int) bool {
	if p.switches(animeID, episode) >= slowSwitches {
		return false
	}
	rec, ok := p.held(ctx, animeID, episode)
	return !ok || (!rec.Manual && !p.ruledOut(rec.InfoHash))
}

func (p *Prefetcher) switchAway(ctx context.Context, animeID, episode, season int, prefs score.Preferences, slow *torrent.SlowError) bool {
	d, err := p.torrent.Details(ctx, slow.ID)
	if err != nil {
		return false
	}
	rec, held := p.held(ctx, animeID, episode)
	percent := slow.Percent
	if stats, err := p.torrent.Stats(ctx, slow.ID); err == nil && held && rec.TotalSize > 0 &&
		rec.FileIndex >= 0 && rec.FileIndex < len(stats.FileProgress) {
		percent = float64(stats.FileProgress[rec.FileIndex]) / float64(rec.TotalSize) * 100
	}
	if percent >= 50 {
		return false
	}
	hash := strings.ToLower(d.InfoHash)
	if playing, err := p.pinned(ctx, ""); err != nil || playing {
		return false
	}
	if !p.alternative(ctx, animeID, episode, season, prefs, hash) {
		p.log.Info("queued release downloads slowly, but nothing else could replace it",
			"anime", animeID, "episode", episode, "release", d.Name)
		return false
	}
	p.ruleOut(animeID, episode, hash)
	if p.store.TorrentEpisodeCount(ctx, hash) > 1 || p.claims(hash) > 0 {
		if held && rec.FileIndex >= 0 && strings.EqualFold(rec.InfoHash, hash) {
			if err := p.torrent.IncludeFile(ctx, slow.ID, rec.FileIndex, false); err != nil {
				p.log.Debug("stop fetching the switched episode", "torrent", slow.ID, "err", err)
			}
		}
		p.stop(context.WithoutCancel(ctx), slow.ID, false, "this episode switched away from", slow)
		p.log.Info("queued release downloads too slowly, switching this episode; the pack keeps what other episodes have",
			"anime", animeID, "episode", episode, "release", d.Name, "kbps", int(slow.Rate/1024))
		return true
	}
	p.log.Info("queued release downloads too slowly, switching",
		"anime", animeID, "episode", episode, "release", d.Name, "kbps", int(slow.Rate/1024), "percent", int(slow.Percent))
	if err := p.torrent.Delete(ctx, slow.ID); err != nil {
		p.log.Warn("remove slow release", "torrent", slow.ID, "err", err)
	}
	if err := p.store.DropTorrentCache(ctx, hash); err != nil {
		p.log.Warn("forget slow release", "torrent", slow.ID, "err", err)
	}
	return true
}

func (p *Prefetcher) alternative(ctx context.Context, animeID, episode, season int, prefs score.Preferences, slow string) bool {
	found, err := p.finder.Find(ctx, Request{AnimeID: animeID, Episode: episode, Season: season, Prefs: prefs})
	if err != nil {
		return false
	}
	need, rank := 1, 0.0
	for _, r := range found.Results {
		if strings.EqualFold(r.Torrent.InfoHash, slow) {
			rank = score.ResolutionRank(r.Release.Resolution, prefs)
			if r.Torrent.SeedersKnown {
				need = min(r.Torrent.Seeders+1, healthySwarm)
			}
		}
	}
	return slices.ContainsFunc(found.Results, func(r score.Result) bool {
		return r.AutoPick && !strings.EqualFold(r.Torrent.InfoHash, slow) && !p.ruledOut(r.Torrent.InfoHash) &&
			!p.pausedByUser(r.Torrent.InfoHash) && r.Torrent.SeedersKnown && r.Torrent.Seeders >= max(need, 1) &&
			score.ResolutionRank(r.Release.Resolution, prefs) >= rank
	})
}

func (p *Prefetcher) keepEpisode(ctx context.Context, hash string, index int) error {
	if index >= 0 {
		if kept, err := p.store.KeepFile(ctx, hash, index, true); err != nil || kept {
			return err
		}
	}
	_, err := p.store.KeepDownload(ctx, hash, true)
	return err
}

func (p *Prefetcher) pinned(ctx context.Context, hash string) (bool, error) {
	entries, err := p.store.CacheEntries(ctx)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.Pinned && (hash == "" || strings.EqualFold(e.InfoHash, hash)) {
			return true, nil
		}
	}
	return false, nil
}

func (p *Prefetcher) downloadOnce(ctx context.Context, animeID, episode, season int, prefs score.Preferences, stall time.Duration, pick string, paced bool) error {
	if rec, ok := p.held(ctx, animeID, episode); ok && (pick == "" || strings.EqualFold(rec.InfoHash, pick)) &&
		(pick != "" || !p.ruledOut(rec.InfoHash)) {
		if pick == "" && p.pausedByUser(rec.InfoHash) {
			return errReleasePaused
		}
		if pick == "" && p.pausedByUser(FileKey(rec.InfoHash, rec.FileIndex)) {
			return errEpisodePaused
		}
		if err := p.keepEpisode(ctx, rec.InfoHash, rec.FileIndex); err != nil {
			return err
		}
		id, running, err := p.resume(ctx, rec)
		switch {
		case errors.Is(err, errNotInEngine):
		case err != nil:
			return err
		case !running:
			return nil
		default:
			done := p.claimWhile(rec.InfoHash)
			defer done()
			return p.awaitPaced(ctx, id, stall, animeID, paced, rec)
		}
	}

	id, started, err := p.fetchRelease(ctx, animeID, episode, season, prefs, true, pick)
	if err != nil || !started {
		return err
	}
	rec, ok := p.held(ctx, animeID, episode)
	if ok {
		done := p.claimWhile(rec.InfoHash)
		defer done()
	} else {
		rec = store.TorrentRecord{FileIndex: -1}
	}
	return p.awaitPaced(ctx, id, stall, animeID, paced, rec)
}

func (p *Prefetcher) awaitPaced(ctx context.Context, id int, stall time.Duration, animeID int, paced bool, rec store.TorrentRecord) error {
	pace := torrent.Pace{File: rec.FileIndex, FileSize: rec.TotalSize}
	if rec.FileIndex < 0 {
		pace.FileSize = 0
	}
	if paced {
		episodeBytes := rec.TotalSize
		if episodeBytes <= 0 {
			if stats, err := p.torrent.Stats(ctx, id); err == nil {
				episodeBytes = stats.TotalBytes
			}
		}
		if episodeBytes > 0 {
			runtime := p.store.EpisodeRuntime(ctx, animeID)
			if runtime <= 0 {
				runtime = 24
			}
			pace.MinRate = slowShare * float64(episodeBytes) / float64(runtime*60)
		}
		if global, err := p.store.Prefs(ctx, 0); err == nil {
			if limit := global.Int64("downloads.limit_bytes"); limit > 0 && float64(limit) < 2*pace.MinRate {
				pace.MinRate = 0
			}
		}
		pace.Window, pace.FewPeers = slowWindow, fewPeers
	}
	err := p.torrent.AwaitPaced(ctx, id, stall, pace)
	var slow *torrent.SlowError
	if err != nil && !errors.Is(err, torrent.ErrPaused) && !errors.As(err, &slow) {
		p.stop(context.WithoutCancel(ctx), id, true, "did not finish", err)
	}
	return err
}

func (p *Prefetcher) stop(ctx context.Context, id int, claimed bool, what string, reason error) {
	if p.playing(ctx, id) {
		p.log.Info("leaving a download that "+what+" running: it is playing", "torrent", id, "reason", reason)
		return
	}
	mine := 0
	if claimed {
		mine = 1
	}
	if d, err := p.torrent.Details(ctx, id); err == nil && p.claims(d.InfoHash) > mine {
		p.log.Info("leaving a download that "+what+" running: another episode downloads from it", "torrent", id, "reason", reason)
		return
	}
	p.log.Info("stopping a download that "+what, "torrent", id, "reason", reason)
	if err := p.torrent.Pause(ctx, id); err != nil {
		p.log.Warn("stop abandoned download", "torrent", id, "err", err)
	}
}

func (p *Prefetcher) playing(ctx context.Context, id int) bool {
	d, err := p.torrent.Details(ctx, id)
	if err != nil || d.InfoHash == "" {
		return false
	}
	playing, _ := p.pinned(ctx, d.InfoHash)
	return playing
}

func localCopy(ctx context.Context, st *store.Store, log *slog.Logger, animeID, episode int) (store.LocalFile, bool) {
	for {
		f, err := st.LocalEpisode(ctx, animeID, episode)
		if err != nil || f.ID == 0 {
			return store.LocalFile{}, false
		}
		if _, err := os.Stat(f.Path); err == nil {
			return f, true
		}
		log.Warn("library file gone; marking it missing", "path", f.Path)
		if err := st.MarkLocalMissing(ctx, f.ID); err != nil {
			return store.LocalFile{}, false
		}
	}
}

var errNotInEngine = errors.New("not held by the engine")

func (p *Prefetcher) resume(ctx context.Context, rec store.TorrentRecord) (id int, running bool, err error) {
	live, err := p.torrent.Live(ctx)
	if err != nil {
		return 0, false, err
	}
	id, ok := live[strings.ToLower(rec.InfoHash)]
	if !ok {
		return 0, false, errNotInEngine
	}
	if rec.FileIndex >= 0 {
		if err := p.torrent.IncludeFile(ctx, id, rec.FileIndex, true); err != nil {
			p.log.Debug("include the episode's file", "torrent", id, "err", err)
		}
	}
	stats, err := p.torrent.Stats(ctx, id)
	if err != nil {
		return 0, false, err
	}
	if _, _, done := (torrent.Pace{File: rec.FileIndex, FileSize: rec.TotalSize}).Progress(stats); done {
		return id, false, nil
	}

	if err := p.torrent.Start(ctx, id); err != nil {
		p.log.Debug("resume part-downloaded episode", "torrent", id, "err", err)
	}
	p.log.Info("resuming part-downloaded episode", "anime", rec.AnimeID, "episode", rec.EpKey)
	return id, true, nil
}

func (p *Prefetcher) inEngine(ctx context.Context, animeID, episode int) bool {
	rec, ok := p.held(ctx, animeID, episode)
	if !ok {
		return false
	}
	live, err := p.torrent.Live(ctx)
	if err != nil {
		return true
	}
	_, ok = live[strings.ToLower(rec.InfoHash)]
	return ok
}

const fetchAttempts = 3

var prefetchWarmCap = 5 * time.Minute

var ErrSeedersSilent = errors.New("the seeders never answered")

func (p *Prefetcher) fetch(ctx context.Context, animeID, episode, season int, prefs score.Preferences, keep bool) (int, bool, error) {
	return p.fetchRelease(ctx, animeID, episode, season, prefs, keep, "")
}

func (p *Prefetcher) fetchRelease(ctx context.Context, animeID, episode, season int, prefs score.Preferences, keep bool, pick string) (int, bool, error) {
	flight, ok := p.beginFetch(animeID, episode)
	if !ok {
		return 0, false, errOvertaken
	}
	defer p.endFetch(animeID, episode, flight)

	prior, _ := p.held(ctx, animeID, episode)
	if pick == "" && p.inEngine(ctx, animeID, episode) && !p.ruledOut(prior.InfoHash) {
		return 0, false, nil
	}

	if _, ok := localCopy(ctx, p.store, p.log, animeID, episode); ok {
		return 0, false, nil
	}

	var usage store.CacheUsage
	if !keep {
		var err error
		if usage, err = p.store.CacheUsage(ctx); err != nil {
			return 0, false, err
		}
		if p.room == nil && usage.Budget > 0 && usage.Bytes >= usage.Budget {
			return 0, false, fmt.Errorf("cache at budget")
		}
	}

	total, _ := p.store.EpisodeCount(ctx, animeID)
	if total > 0 && episode > total {
		return 0, false, nil
	}

	var best score.Result
	var inspected, added *torrent.Torrent
	var file torrent.File
	var index int
	lastErr := fmt.Errorf("no release found")
	var stale string
	allowRuled, skippedRuled, skippedPaused := pick != "", false, false
	addFirst := func(results []score.Result) error {
		tried := 0
		for _, cand := range results {
			if (!cand.AutoPick && pick == "") || tried >= fetchAttempts || cand.Torrent.InfoHash == stale {
				continue
			}
			if pick == "" && p.pausedByUser(cand.Torrent.InfoHash) {
				skippedPaused = true
				continue
			}
			if ruled := p.ruledOut(cand.Torrent.InfoHash); pick == "" && ruled != allowRuled {
				skippedRuled = skippedRuled || ruled
				continue
			}
			if !keep && usage.Budget > 0 && usage.Bytes+cand.EpisodeBytes() > usage.Budget {
				if p.room == nil || !p.room(ctx, cand.EpisodeBytes(), animeID) {
					return fmt.Errorf("would exceed cache budget")
				}
				if after, err := p.store.CacheUsage(ctx); err == nil {
					usage = after
				}
			}
			tried++

			inspect := p.torrent.Inspect
			if pick != "" {
				inspect = p.torrent.InspectPicked
			}
			ins, err := inspect(ctx, cand.Torrent.Magnet())
			if err != nil {
				lastErr = err
				continue
			}
			f, i, ok := pickFile(ins.Details.Files, cand)
			if !ok {
				lastErr = fmt.Errorf("episode not in torrent")
				continue
			}
			a, err := p.torrent.Add(ctx, cand.Torrent.Magnet(), f)
			if err != nil {
				lastErr = err
				continue
			}
			best, inspected, file, index, added = cand, ins, f, i, a
			return nil
		}
		return nil
	}

	torrentRecord := func() store.TorrentRecord {
		return store.TorrentRecord{
			InfoHash:  best.Torrent.InfoHash,
			EngineID:  added.ID,
			Name:      inspected.Details.Name,
			TotalSize: file.Length,
			AnimeID:   animeID,
			EpKey:     epKey(episode),
			FileIndex: index,
			FilePath:  file.Name,
			Manual:    pick != "",
		}
	}

	record := func(ctx context.Context) error {
		if rec, ok := p.held(ctx, animeID, episode); ok && !strings.EqualFold(rec.InfoHash, best.Torrent.InfoHash) &&
			!strings.EqualFold(rec.InfoHash, prior.InfoHash) {
			return errOvertaken
		}
		if err := p.store.RecordTorrent(ctx, torrentRecord()); err != nil {
			return err
		}
		if keep {
			if err := p.keepEpisode(ctx, best.Torrent.InfoHash, index); err != nil {
				return err
			}
		}
		return nil
	}

	start := func(keepDead bool) error {
		p.claim(flight, best.Torrent.InfoHash)
		p.setTorrent(flight, torrentRecord())
		err := p.torrent.WaitLive(ctx, added.ID, 2*time.Minute)
		if err == nil {
			warm, cancel := context.WithTimeout(ctx, prefetchWarmCap)
			err = p.torrent.Prewarm(warm, added.ID, index, 2<<20)
			cancel()
			if err != nil && ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
				err = fmt.Errorf("%w: %v", ErrSeedersSilent, err)
			}
		}
		if err != nil && p.takenOver(flight) {
			return errOvertaken
		}
		if err != nil && (ctx.Err() != nil || keepDead) {
			switch rerr := record(context.WithoutCancel(ctx)); {
			case errors.Is(rerr, errOvertaken):
				p.setTorrent(flight, store.TorrentRecord{})
				p.discardIfAlone(context.WithoutCancel(ctx), added.ID, best.Torrent.InfoHash)
				return err
			case rerr != nil:
				p.log.Warn("record interrupted download", "err", rerr)
			}
		}
		if err != nil {
			p.stop(context.WithoutCancel(ctx), added.ID, true, "did not start", err)
		}
		return err
	}

	if pick == "" {
		p.awaitResolve(ctx, animeID, episode, time.After(prepareWait))
	}
	if rel, ok := p.takePreparedUnless(animeID, episode, pick != ""); ok && !p.ruledOut(rel.Torrent.InfoHash) {
		if err := addFirst([]score.Result{rel}); err != nil {
			p.storePrepared(animeID, episode, rel)
			return 0, false, err
		}
		if added != nil {
			err := start(false)
			if err != nil && (ctx.Err() != nil || errors.Is(err, errOvertaken)) {
				return 0, false, err
			}
			if err != nil {
				p.log.Info("release resolved ahead did not start, searching", "anime", animeID, "episode", episode)
				p.setTorrent(flight, store.TorrentRecord{})
				p.discardIfAlone(context.WithoutCancel(ctx), added.ID, rel.Torrent.InfoHash)
				stale, added, lastErr = rel.Torrent.InfoHash, nil, err
			}
		}
	}
	if added == nil {
		byHash := false
		found, err := p.finder.Find(ctx, Request{
			AnimeID: animeID, Episode: episode, Season: season, Prefs: prefs,
		})
		if err != nil {
			return 0, false, err
		}
		results := found.Results
		if pick != "" {
			results = slices.DeleteFunc(slices.Clone(results), func(r score.Result) bool {
				return !strings.EqualFold(r.Torrent.InfoHash, pick)
			})
			if len(results) == 0 {
				byHash = true
				scope := p.finder.PackScope(ctx, animeID, episode, season)
				results = []score.Result{{Candidate: score.Candidate{
					Torrent: indexer.Torrent{InfoHash: strings.ToLower(pick), Indexer: "nyaa"},
					Release: parse.Release{Batch: true},
					Episode: episode,
					Numbers: fileNumbers(ctx, p.store, animeID, episode),
					Season:  scope.Season,
					Counted: scope.Counted,
					Others:  scope.Others,
				}}}
			}
		}
		if err := addFirst(results); err != nil {
			return 0, false, err
		}
		if added == nil && skippedRuled {
			allowRuled = true
			if err := addFirst(results); err != nil {
				return 0, false, err
			}
		}
		if more := score.Fallbacks(results); added == nil && pick == "" && !skippedPaused && len(more) > 0 {
			allowRuled = false
			prior := lastErr
			if err := addFirst(more); err != nil {
				return 0, false, err
			}
			if added == nil && swarmFailure(prior) {
				lastErr = prior
			}
		}
		if added == nil && byHash {
			return 0, false, fmt.Errorf("release %s not found: no search lists it, and by its hash: %w", pick, lastErr)
		}
		if added == nil && skippedPaused {
			return 0, false, errReleasePaused
		}
		if added == nil {
			return 0, false, lastErr
		}
		if err := start(true); err != nil {
			return 0, false, err
		}
	}

	switch err := record(ctx); {
	case errors.Is(err, errOvertaken):
		p.log.Info("another release was recorded for the episode meanwhile; this one goes",
			"anime", animeID, "episode", episode)
		p.setTorrent(flight, store.TorrentRecord{})
		p.discardIfAlone(context.WithoutCancel(ctx), added.ID, best.Torrent.InfoHash)
		return 0, false, err
	case err != nil:
		return added.ID, true, err
	}
	dropSuperseded(ctx, p.store, p.torrent, p.log, animeID, epKey(episode))

	p.log.Info("fetched episode",
		"anime", animeID, "episode", episode, "group", best.Release.Group, "kept", keep)
	return added.ID, true, nil
}
