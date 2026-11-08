package library

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"yoru/internal/store"
	"yoru/internal/torrent"
)

type Cache struct {
	store   *store.Store
	torrent *torrent.Client
	dir     string
	log     *slog.Logger

	mu     sync.Mutex
	absent map[string]*absence

	progress map[string]mark

	seen map[string]time.Time

	claimed func(hash string) bool

	protected []string

	sweeping sync.Mutex
}

func (c *Cache) WithProtected(dirs ...string) *Cache {
	for _, d := range dirs {
		if abs, err := filepath.Abs(d); err == nil {
			c.protected = append(c.protected, abs)
		}
	}
	return c
}

func (c *Cache) isProtected(path string) bool {
	for _, p := range c.protected {
		if torrent.Within(path, p) {
			return true
		}
	}
	return false
}

func (c *Cache) WithClaimed(claimed func(hash string) bool) *Cache {
	c.claimed = claimed
	return c
}

type mark struct {
	bytes int64
	since time.Time
}

var stalledAfter = 12 * time.Hour

var adoptAfter = 2 * time.Minute

func (c *Cache) watch(hash string, bytes int64, done bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	hash = strings.ToLower(hash)
	if done {
		delete(c.progress, hash)
		return
	}
	if m, ok := c.progress[hash]; ok && m.bytes == bytes {
		return
	}
	c.progress[hash] = mark{bytes: bytes, since: time.Now()}
}

func (c *Cache) unwatch(hash string) {
	c.mu.Lock()
	delete(c.progress, strings.ToLower(hash))
	c.mu.Unlock()
}

func (c *Cache) stalled() map[string]bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[string]bool{}
	for hash, m := range c.progress {
		if time.Since(m.since) >= stalledAfter {
			out[hash] = true
		}
	}
	return out
}

type absence struct {
	count int
	since time.Time
}

func NewCache(s *store.Store, tc *torrent.Client, dir string, log *slog.Logger) *Cache {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return &Cache{store: s, torrent: tc, dir: dir, log: log,
		absent: map[string]*absence{}, progress: map[string]mark{}}
}

func (c *Cache) owns(d torrent.Detail) bool {
	return torrent.Within(c.dir, d.OutputFolder)
}

type SweepReport struct {
	Before  int64 `json:"before"`
	After   int64 `json:"after"`
	Budget  int64 `json:"budget"`
	Evicted int   `json:"evicted"`
	Freed   int64 `json:"freed"`
}

func (c *Cache) Sweep(ctx context.Context) (SweepReport, error) {
	c.sweeping.Lock()
	defer c.sweeping.Unlock()
	if err := c.refresh(ctx); err != nil {
		c.log.Warn("refresh cache sizes", "err", err)
	}
	if eps, err := c.store.SupersededEpisodes(ctx); err == nil {
		for _, ep := range eps {
			dropSuperseded(ctx, c.store, c.torrent, c.log, ep.AnimeID, ep.EpKey)
		}
	}

	usage, err := c.store.CacheUsage(ctx)
	if err != nil {
		return SweepReport{}, err
	}

	rep := SweepReport{Before: usage.Bytes, After: usage.Bytes, Budget: usage.Budget}
	if usage.Budget <= 0 || usage.Bytes <= usage.Budget {
		return rep, nil
	}
	order := slices.DeleteFunc(evictionOrder(usage.Entries, c.stalled(), c.lastPlayed(ctx)), c.fetching)
	c.evictDownTo(ctx, &rep, usage.Budget, order, torrentBytes(usage.Entries))

	if rep.Evicted > 0 {
		c.log.Info("cache swept",
			"evicted", rep.Evicted,
			"freedMB", rep.Freed>>20,
			"usedMB", rep.After>>20,
			"budgetMB", usage.Budget>>20)
	}
	return rep, nil
}

func torrentBytes(entries []store.CacheEntry) map[string]int64 {
	freed := map[string]int64{}
	for _, e := range entries {
		freed[e.InfoHash] += e.Bytes
	}
	return freed
}

func (c *Cache) evictDownTo(ctx context.Context, rep *SweepReport, target int64, order []store.CacheEntry, freed map[string]int64) {
	gone := map[string]bool{}
	for _, e := range order {
		if rep.After <= target {
			break
		}
		if gone[e.InfoHash] {
			continue
		}
		if err := c.evict(ctx, e); err != nil {
			c.log.Warn("evict", "hash", e.InfoHash, "err", err)
			continue
		}
		gone[e.InfoHash] = true
		rep.Evicted++
		rep.Freed += freed[e.InfoHash]
		rep.After -= freed[e.InfoHash]
	}
}

func (c *Cache) MakeRoom(ctx context.Context, need int64, animeID int) bool {
	c.sweeping.Lock()
	defer c.sweeping.Unlock()

	usage, err := c.store.CacheUsage(ctx)
	if err != nil {
		return false
	}
	target := usage.Budget - need
	if usage.Budget <= 0 || usage.Bytes <= target {
		return true
	}
	watched, err := c.store.WatchedEpisodes(ctx, animeID)
	if err != nil {
		return false
	}

	spared := map[string]bool{}
	for _, e := range usage.Entries {
		if e.AnimeID == nil || *e.AnimeID != animeID {
			continue
		}
		if n, err := strconv.Atoi(e.EpKey); err != nil || !watched[n] {
			spared[e.InfoHash] = true
		}
	}
	freed := torrentBytes(usage.Entries)
	order := slices.DeleteFunc(evictionOrder(usage.Entries, c.stalled(), c.lastPlayed(ctx)), func(e store.CacheEntry) bool {
		return spared[e.InfoHash] || c.fetching(e)
	})
	var available int64
	counted := map[string]bool{}
	for _, e := range order {
		if !counted[e.InfoHash] {
			counted[e.InfoHash] = true
			available += freed[e.InfoHash]
		}
	}
	if usage.Bytes-available > target {
		return false
	}

	rep := SweepReport{Before: usage.Bytes, After: usage.Bytes, Budget: usage.Budget}
	c.evictDownTo(ctx, &rep, target, order, freed)
	if rep.Evicted > 0 {
		c.log.Info("cache made room for the next episode",
			"anime", animeID, "evicted", rep.Evicted, "freedMB", rep.Freed>>20, "usedMB", rep.After>>20)
	}
	return rep.After <= target
}

type episodeRef struct {
	animeID int
	epKey   string
}

func (c *Cache) lastPlayed(ctx context.Context) episodeRef {
	animeID, epKey, ok := c.store.LastPlayed(ctx)
	if !ok {
		return episodeRef{}
	}
	return episodeRef{animeID: animeID, epKey: epKey}
}

func (c *Cache) fetching(e store.CacheEntry) bool {
	return c.claimed != nil && c.claimed(strings.ToLower(e.InfoHash))
}

func evictionOrder(entries []store.CacheEntry, stalled map[string]bool, watching episodeRef) []store.CacheEntry {
	held := map[string]bool{}
	for _, e := range entries {
		inProgress := e.AnimeID != nil && *e.AnimeID == watching.animeID && e.EpKey == watching.epKey
		if e.Pinned || e.Kept || inProgress || (!e.Complete && !stalled[strings.ToLower(e.InfoHash)]) {
			held[e.InfoHash] = true
		}
	}

	var plain, protected []store.CacheEntry
	for _, e := range entries {
		switch {
		case held[e.InfoHash]:
			continue
		case e.Protected:
			protected = append(protected, e)
		default:
			plain = append(plain, e)
		}
	}

	latest := map[string]int64{}
	for _, e := range entries {
		latest[e.InfoHash] = max(latest[e.InfoHash], e.LastPlayed)
	}
	byAge := func(list []store.CacheEntry) {
		slices.SortStableFunc(list, func(a, b store.CacheEntry) int { return cmp.Compare(latest[a.InfoHash], latest[b.InfoHash]) })
	}
	byAge(plain)
	byAge(protected)

	return append(plain, protected...)
}

func (c *Cache) AutoDelete(ctx context.Context, animeID int) (int, error) {
	if animeID == 0 {
		return 0, nil
	}
	removed, err := c.autoDeleteShow(ctx, animeID)
	if err != nil {
		return removed, err
	}
	entries, err := c.store.CacheEntries(ctx)
	if err != nil {
		return removed, err
	}
	others := map[int]bool{}
	for _, e := range entries {
		if e.AnimeID != nil && *e.AnimeID != animeID && *e.AnimeID != 0 {
			others[*e.AnimeID] = true
		}
	}
	for other := range others {
		n, err := c.autoDeleteShow(ctx, other)
		if err != nil {
			c.log.Warn("auto-delete watched", "anime", other, "err", err)
		}
		removed += n
	}
	return removed, nil
}

func (c *Cache) autoDeleteShow(ctx context.Context, animeID int) (int, error) {
	prefs, err := c.store.Prefs(ctx, animeID)
	if err != nil {
		return 0, err
	}
	mode := prefs.String("cache.autodelete")
	if mode != "now" && mode != "keep2" {
		return 0, nil
	}
	downloads := prefs.Bool("cache.autodelete_downloads")

	watched, err := c.store.WatchedEpisodes(ctx, animeID)
	if err != nil {
		return 0, err
	}
	later, err := c.store.WatchedAfter(ctx, animeID)
	if err != nil {
		return 0, err
	}
	entries, err := c.store.CacheEntries(ctx)
	if err != nil {
		return 0, err
	}

	done := func(ep int) bool {
		return watched[ep] && (mode == "now" || (watched[ep+1] && watched[ep+2]) || later[ep] >= 2)
	}
	veto := map[string]bool{}
	candidates := map[string]bool{}
	doneFiles := map[string]map[int]bool{}
	for _, e := range entries {
		if e.AnimeID == nil || *e.AnimeID != animeID || c.fetching(e) {
			veto[e.InfoHash] = true
			continue
		}
		ep, err := strconv.Atoi(e.EpKey)
		if err != nil || e.Pinned || (e.Kept && !downloads) || !done(ep) {
			veto[e.InfoHash] = true
			continue
		}
		candidates[e.InfoHash] = true
		if doneFiles[e.InfoHash] == nil {
			doneFiles[e.InfoHash] = map[int]bool{}
		}
		doneFiles[e.InfoHash][e.FileIndex] = true
	}

	removed := 0
	for hash := range candidates {
		if veto[hash] || c.holdsUnrecorded(ctx, hash, doneFiles[hash]) {
			continue
		}
		if err := c.evict(ctx, store.CacheEntry{InfoHash: hash}); err != nil {
			c.log.Warn("auto-delete watched", "hash", hash, "err", err)
			continue
		}
		removed++
	}
	if removed > 0 {
		c.log.Info("watched episodes removed", "anime", animeID, "count", removed, "rule", mode)
	}
	return removed, nil
}

func (c *Cache) holdsUnrecorded(ctx context.Context, hash string, watched map[int]bool) bool {
	id, ok := c.liveID(ctx, hash)
	if !ok {
		return false
	}
	d, err := c.torrent.Details(ctx, id)
	if err != nil {
		return true
	}
	for i, f := range d.Files {
		if f.Included && !watched[i] {
			return true
		}
	}
	return false
}

func dropSuperseded(ctx context.Context, st *store.Store, tc *torrent.Client, log *slog.Logger, animeID int, epKey string) {
	hashes, err := st.Superseded(ctx, animeID, epKey)
	if err != nil {
		return
	}
	files, err := st.SupersededFiles(ctx, animeID, epKey)
	if err != nil || len(hashes)+len(files) == 0 {
		return
	}
	live := map[string]int{}
	if tc != nil {
		if l, err := tc.Live(ctx); err == nil {
			live = l
		}
	}
	for _, hash := range hashes {
		if ready, err := st.SupersededReady(ctx, animeID, epKey, hash); err != nil || !ready {
			continue
		}
		if id, ok := live[strings.ToLower(hash)]; ok {
			if err := tc.Delete(ctx, id); err != nil {
				log.Warn("remove superseded release", "hash", hash, "err", err)
				continue
			}
		}
		if err := st.DropTorrentCache(ctx, hash); err != nil {
			log.Warn("forget superseded release", "hash", hash, "err", err)
			continue
		}
		log.Info("superseded release removed", "anime", animeID, "episode", epKey, "hash", hash)
	}
	for _, f := range files {
		id, inEngine := live[strings.ToLower(f.InfoHash)]
		if !inEngine {
			continue
		}
		if ready, err := st.SupersededFileReady(ctx, animeID, epKey, f); err != nil || !ready {
			continue
		}
		if err := tc.DeleteFile(ctx, id, f.FileIndex); err != nil {
			log.Warn("remove a superseded episode from a pack", "hash", f.InfoHash, "file", f.FileIndex, "err", err)
			continue
		}
		if err := st.DropTorrentFile(ctx, f.InfoHash, f.FileIndex); err != nil {
			log.Warn("forget a superseded episode of a pack", "hash", f.InfoHash, "file", f.FileIndex, "err", err)
			continue
		}
		if err := st.DropFileCache(ctx, f.InfoHash, f.FileIndex); err != nil {
			log.Warn("forget a superseded episode's bytes", "hash", f.InfoHash, "file", f.FileIndex, "err", err)
		}
		log.Info("superseded episode removed from a pack the other episodes still use",
			"anime", animeID, "episode", epKey, "hash", f.InfoHash, "file", f.FileIndex)
	}
}

func (c *Cache) Keep(ctx context.Context, infoHash string, kept bool) error {
	found, err := c.store.KeepDownload(ctx, infoHash, kept)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no download for %s", infoHash)
	}
	return nil
}

func (c *Cache) Remove(ctx context.Context, infoHash string) (int64, error) {
	entries, err := c.store.CacheEntries(ctx)
	if err != nil {
		return 0, err
	}

	var freed int64
	var found bool
	for _, e := range entries {
		if !strings.EqualFold(e.InfoHash, infoHash) {
			continue
		}
		if e.Pinned {
			return 0, fmt.Errorf("that episode is playing right now")
		}
		freed += e.Bytes
		found = true
	}
	if !found {
		if id, ok := c.liveID(ctx, infoHash); ok {
			d, err := c.torrent.Details(ctx, id)
			if err != nil {
				return 0, err
			}
			if !c.owns(d) {
				return 0, fmt.Errorf("that download is not in Yoru's cache folder")
			}
			return 0, c.torrent.Delete(ctx, id)
		}
		return 0, fmt.Errorf("no download for %s", infoHash)
	}

	return freed, c.evict(ctx, store.CacheEntry{InfoHash: infoHash})
}

func (c *Cache) Clear(ctx context.Context, completedOnly bool) (int, int64, error) {
	entries, err := c.store.CacheEntries(ctx)
	if err != nil {
		return 0, 0, err
	}

	held := map[string]bool{}
	for _, e := range entries {
		if e.Pinned || e.Kept || (completedOnly && !e.Complete) || c.fetching(e) {
			held[strings.ToLower(e.InfoHash)] = true
		}
	}

	var removed int
	var freed int64
	done := map[string]bool{}
	bytes := torrentBytes(entries)

	for _, e := range entries {
		hash := strings.ToLower(e.InfoHash)
		if held[hash] || done[hash] {
			continue
		}
		if err := c.evict(ctx, e); err != nil {
			c.log.Warn("clear download", "hash", hash, "err", err)
			continue
		}
		done[hash] = true
		removed++
		freed += bytes[e.InfoHash]
	}
	return removed, freed, nil
}

func (c *Cache) liveID(ctx context.Context, infoHash string) (int, bool) {
	if c.torrent == nil {
		return 0, false
	}
	live, err := c.torrent.List(ctx)
	if err != nil {
		return 0, false
	}
	for _, t := range live.Torrents {
		if strings.EqualFold(t.InfoHash, infoHash) {
			return t.ID, true
		}
	}
	return 0, false
}

type Progress struct {
	Bytes    int64 `json:"bytes"`
	Total    int64 `json:"total"`
	Finished bool  `json:"finished"`
	Paused   bool  `json:"paused"`

	Checking bool `json:"checking"`

	Mbps  float64 `json:"mbps"`
	Peers int     `json:"peers"`

	Warning string `json:"warning,omitempty"`

	Files    []int64 `json:"-"`
	Included []bool  `json:"-"`
}

func (c *Cache) Pause(ctx context.Context, infoHash string) error {
	return c.setRunning(ctx, infoHash, false)
}

func (c *Cache) Resume(ctx context.Context, infoHash string) error {
	return c.setRunning(ctx, infoHash, true)
}

func (c *Cache) setRunning(ctx context.Context, infoHash string, run bool) error {
	if c.torrent == nil {
		return fmt.Errorf("torrent engine unavailable")
	}

	live, err := c.torrent.Live(ctx)
	if err != nil {
		return err
	}
	id, ok := live[strings.ToLower(infoHash)]
	if !ok {
		return fmt.Errorf("not held by the engine")
	}

	if run {
		return c.torrent.Start(ctx, id)
	}
	return c.torrent.Pause(ctx, id)
}

func (c *Cache) Swarm(ctx context.Context, infoHash string) (mbps float64, peers int) {
	if c.torrent == nil {
		return 0, 0
	}
	id, ok := c.liveID(ctx, infoHash)
	if !ok {
		return 0, 0
	}
	stats, err := c.torrent.Stats(ctx, id)
	if err != nil || stats.Live == nil {
		return 0, 0
	}
	return megabits(stats.Live.DownloadSpeed.Mbps), stats.Live.Snapshot.PeerStats.Live
}

func (c *Cache) Loaded(ctx context.Context, infoHash string, fileIndex int) (torrent.FileRanges, bool) {
	if c.torrent == nil {
		return torrent.FileRanges{}, false
	}
	id, ok := c.liveID(ctx, infoHash)
	if !ok {
		return torrent.FileRanges{}, false
	}
	ranges, err := c.torrent.Ranges(ctx, id, fileIndex)
	return ranges, err == nil
}

func megabits(mibps float64) float64 { return mibps * 8 * 1.048576 }

func (c *Cache) Finished(ctx context.Context, infoHash string) bool {
	if c.torrent == nil {
		return false
	}
	id, ok := c.liveID(ctx, infoHash)
	if !ok {
		return false
	}
	stats, err := c.torrent.Stats(ctx, id)
	return err == nil && stats.Finished
}

func (c *Cache) Downloaded(ctx context.Context, infoHash string) int64 {
	if c.torrent == nil {
		return 0
	}
	live, err := c.torrent.Live(ctx)
	if err != nil {
		return 0
	}
	id, ok := live[strings.ToLower(infoHash)]
	if !ok {
		return 0
	}
	stats, err := c.torrent.Stats(ctx, id)
	if err != nil {
		return 0
	}
	return stats.ProgressBytes
}

func (c *Cache) Progress(ctx context.Context) (map[string]Progress, error) {
	if c.torrent == nil {
		return nil, nil
	}
	live, err := c.torrent.List(ctx)
	if err != nil {
		return nil, err
	}

	out := make(map[string]Progress, len(live.Torrents))
	for _, t := range live.Torrents {
		stats, err := c.torrent.Stats(ctx, t.ID)
		if err != nil {
			continue
		}
		p := Progress{
			Bytes:    stats.ProgressBytes,
			Total:    stats.TotalBytes,
			Finished: stats.Finished,
			Checking: stats.Checking(),

			Paused:  stats.Live == nil && !stats.Finished && !stats.Checking(),
			Warning: stats.Warning,
			Files:   stats.FileProgress,
		}
		if stats.Live != nil {
			p.Mbps = megabits(stats.Live.DownloadSpeed.Mbps)
			p.Peers = stats.Live.Snapshot.PeerStats.Live
		}
		p.Included = stats.FileIncluded
		out[strings.ToLower(t.InfoHash)] = p
	}
	return out, nil
}

const forgetAfter = 2

var forgetAge = 10 * time.Minute

func (c *Cache) missing(hash string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	a := c.absent[hash]
	if a == nil {
		a = &absence{since: time.Now()}
		c.absent[hash] = a
	}
	a.count++
	return a.count >= forgetAfter && time.Since(a.since) >= forgetAge
}

func (c *Cache) present(hashes map[string]struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for hash := range hashes {
		delete(c.absent, hash)
	}
}

func (c *Cache) forgetVanished(ctx context.Context) error {
	live, err := c.torrent.List(ctx)
	if err != nil {
		return err
	}

	held := make(map[string]struct{}, len(live.Torrents))
	for _, t := range live.Torrents {
		held[strings.ToLower(t.InfoHash)] = struct{}{}
	}
	c.present(held)

	entries, err := c.store.CacheEntries(ctx)
	if err != nil {
		return err
	}
	if len(held) == 0 {
		return nil
	}

	gone := map[string][]store.CacheEntry{}
	for _, e := range entries {
		hash := strings.ToLower(e.InfoHash)
		if _, ok := held[hash]; !ok {
			gone[hash] = append(gone[hash], e)
		}
	}

	var dropped int
	for hash, rows := range gone {
		if !c.missing(hash) || c.keptOnDisk(rows) {
			continue
		}
		if err := c.store.DropTorrentCache(ctx, rows[0].InfoHash); err != nil {
			return err
		}
		dropped++
	}
	if dropped > 0 {
		c.log.Info("forgot downloads the engine no longer holds", "count", dropped)
	}
	return nil
}

func (c *Cache) keptOnDisk(rows []store.CacheEntry) bool {
	for _, e := range rows {
		if e.Kept && e.Name != "" {
			if _, err := os.Stat(filepath.Join(c.dir, e.Name)); err == nil {
				return true
			}
		}
	}
	return false
}

func (c *Cache) evict(ctx context.Context, e store.CacheEntry) error {
	if c.torrent != nil {
		live, err := c.torrent.Live(ctx)
		if err != nil {
			return err
		}
		if id, ok := live[strings.ToLower(e.InfoHash)]; ok {
			d, err := c.torrent.Details(ctx, id)
			if err != nil {
				return err
			}
			if !c.owns(d) {
				c.log.Warn("not deleting a download outside the cache folder", "hash", e.InfoHash, "folder", d.OutputFolder)
			} else if err := c.torrent.Delete(ctx, id); err != nil {
				if errors.Is(err, torrent.ErrUnavailable) {
					return err
				}
				c.log.Warn("delete torrent", "id", id, "err", err)
			}
		} else if err := c.removeByName(ctx, e, live); err != nil {
			c.log.Warn("delete download files", "hash", e.InfoHash, "err", err)
		}
	}
	return c.store.DropTorrentCache(ctx, e.InfoHash)
}

func (c *Cache) removeByName(ctx context.Context, e store.CacheEntry, live map[string]int) error {
	name := e.Name
	if name == "" {
		if rows, err := c.store.CacheEntries(ctx); err == nil {
			for _, r := range rows {
				if strings.EqualFold(r.InfoHash, e.InfoHash) && r.Name != "" {
					name = r.Name
					break
				}
			}
		}
	}
	if name == "" {
		return nil
	}
	var refused error
	for _, n := range torrent.DiskNames(name, e.InfoHash) {
		found, err := c.removeNamed(ctx, n, live)
		if errors.Is(err, errNotOwnName) {
			refused = err
			continue
		}
		if found || err != nil {
			return err
		}
	}
	return refused
}

var errNotOwnName = errors.New("not a download's own name; nothing deleted")

func (c *Cache) removeNamed(ctx context.Context, name string, live map[string]int) (found bool, err error) {
	first := strings.ToLower(strings.Split(filepath.ToSlash(filepath.Clean(name)), "/")[0])
	if !filepath.IsLocal(name) || torrent.OwnFolder(first) {
		return false, fmt.Errorf("%q: %w", name, errNotOwnName)
	}
	path := filepath.Join(c.dir, name)
	if !torrent.Within(c.dir, path) || filepath.Clean(path) == filepath.Clean(c.dir) {
		return false, fmt.Errorf("%q is outside the cache: %w", name, errNotOwnName)
	}
	if _, err := os.Stat(path); err != nil {
		return false, nil
	}
	for _, id := range live {
		d, err := c.torrent.Details(ctx, id)
		if err != nil {
			continue
		}
		if filepath.Clean(d.OutputFolder) == filepath.Clean(path) {
			return true, nil
		}
		for _, f := range d.Files {
			if strings.EqualFold(filepath.Join(d.OutputFolder, onDisk(f)), path) {
				return true, nil
			}
		}
	}
	root, err := os.OpenRoot(c.dir)
	if err != nil {
		return true, err
	}
	defer root.Close()
	return true, root.RemoveAll(name)
}

func (c *Cache) refresh(ctx context.Context) error {
	if c.torrent == nil {
		return nil
	}
	if err := c.adopt(ctx); err != nil {
		c.log.Warn("adopt untracked torrents", "err", err)
	}
	if err := c.forgetVanished(ctx); err != nil {
		c.log.Warn("forget vanished downloads", "err", err)
	}

	entries, err := c.store.CacheEntries(ctx)
	if err != nil {
		return err
	}
	live, err := c.torrent.Live(ctx)
	if err != nil {
		return err
	}

	seenStats := map[int]torrent.Stats{}
	seenDetail := map[int]torrent.Detail{}

	for _, e := range entries {
		id, ok := live[strings.ToLower(e.InfoHash)]
		if !ok {
			continue
		}
		stats, known := seenStats[id]
		if !known {
			s, err := c.torrent.Stats(ctx, id)
			if err != nil {
				continue
			}
			stats, seenStats[id] = s, s
		}

		detail, known := seenDetail[id]
		if !known {
			d, err := c.torrent.Details(ctx, id)
			if err != nil {
				c.log.Warn("measure download", "id", id, "err", err)
				continue
			}
			detail, seenDetail[id] = d, d
		}

		bytes, done := heldBytes(detail, e.FileIndex), stats.FileDone(detail, e.FileIndex)
		if e.FileIndex == store.WholeTorrent {
			bytes, done = max(bytes, stats.ProgressBytes), stats.Finished
		}
		if stats.State == "live" {
			c.watch(e.InfoHash, stats.ProgressBytes, stats.Finished)
		} else {
			c.unwatch(e.InfoHash)
		}

		if err := c.store.SetCacheBytes(ctx, e.InfoHash, e.FileIndex, bytes, done); err != nil {
			return err
		}
	}
	return nil
}

func onDisk(f torrent.File) string {
	if len(f.Components) > 0 {
		return filepath.Join(f.Components...)
	}
	return filepath.FromSlash(f.Name)
}

func heldBytes(detail torrent.Detail, fileIndex int) int64 {
	var total int64
	for i, f := range detail.Files {
		if fileIndex == store.WholeTorrent && !f.Included {
			continue
		}
		if fileIndex != store.WholeTorrent && i != fileIndex {
			continue
		}

		path := filepath.Join(detail.OutputFolder, onDisk(f))
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		total += allocatedSize(path, info)
	}
	return total
}

func (c *Cache) adopt(ctx context.Context) error {
	live, err := c.torrent.List(ctx)
	if err != nil {
		return err
	}

	now := time.Now()
	c.mu.Lock()
	seen := make(map[string]time.Time, len(live.Torrents))
	for _, t := range live.Torrents {
		hash := strings.ToLower(t.InfoHash)
		if at, ok := c.seen[hash]; ok {
			seen[hash] = at
		} else {
			seen[hash] = now
		}
	}
	c.seen = seen
	c.mu.Unlock()

	known, err := c.store.CacheEntries(ctx)
	if err != nil {
		return err
	}
	tracked := make(map[string]struct{}, len(known))
	for _, e := range known {
		tracked[strings.ToLower(e.InfoHash)] = struct{}{}
	}

	var added int
	for _, t := range live.Torrents {
		hash := strings.ToLower(t.InfoHash)
		if hash == "" {
			continue
		}
		if _, ok := tracked[hash]; ok {
			continue
		}
		if now.Sub(seen[hash]) < adoptAfter {
			continue
		}
		if c.claimed != nil && c.claimed(hash) {
			continue
		}
		if d, err := c.torrent.Details(ctx, t.ID); err != nil || !c.owns(d) {
			continue
		}
		if err := c.store.TrackTorrent(ctx, hash, t.ID, t.Name); err != nil {
			c.log.Warn("track torrent", "hash", hash, "err", err)
			continue
		}
		added++
	}
	if added > 0 {
		c.log.Info("adopted untracked downloads", "count", added)
	}
	return nil
}

const EngineSessionDirName = torrent.EngineSessionDir

type Orphan struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
}

var orphanSettle = 3 * time.Second

func (c *Cache) Orphans(ctx context.Context, dryRun bool) ([]Orphan, error) {
	return c.orphans(ctx, dryRun, nil)
}

func (c *Cache) RemoveOrphans(ctx context.Context, names []string) ([]Orphan, error) {
	only := make(map[string]bool, len(names))
	for _, n := range names {
		only[n] = true
	}
	return c.orphans(ctx, false, only)
}

func (c *Cache) orphans(ctx context.Context, dryRun bool, only map[string]bool) ([]Orphan, error) {
	list, err := c.torrent.List(ctx)
	if err != nil {
		return nil, err
	}
	known, err := c.store.TorrentNames(ctx)
	if err != nil {
		return nil, err
	}
	torrents := list.Torrents
	if !dryRun {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(orphanSettle):
		}
		again, err := c.torrent.List(ctx)
		if err != nil {
			return nil, err
		}
		torrents = append(torrents, again.Torrents...)
	}

	keep := map[string]bool{}
	for _, k := range known {
		keep[strings.ToLower(k.Name)] = true
		for _, name := range torrent.DiskNames(k.Name, k.InfoHash) {
			keep[strings.ToLower(name)] = true
		}
		if host, ok := torrent.StreamHost(k.Name); ok {
			keep[strings.ToLower(host)] = true
		}
	}
	for _, t := range torrents {
		keep[strings.ToLower(t.Name)] = true
		d, err := c.torrent.Details(ctx, t.ID)
		if err != nil {
			return nil, err
		}
		if rel, err := filepath.Rel(c.dir, d.OutputFolder); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			keep[strings.ToLower(strings.Split(filepath.ToSlash(rel), "/")[0])] = true
			continue
		}
		for _, f := range d.Files {
			if len(f.Components) > 0 {
				keep[strings.ToLower(f.Components[0])] = true
			}
			keep[strings.ToLower(f.Name)] = true
		}
	}

	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return nil, err
	}

	root, err := os.OpenRoot(c.dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()

	out := []Orphan{}
	for _, e := range entries {
		name := e.Name()
		if torrent.OwnFolder(name) || keep[strings.ToLower(name)] {
			continue
		}
		if only != nil && !only[name] {
			continue
		}
		path := filepath.Join(c.dir, name)
		if c.isProtected(path) {
			continue
		}
		o := Orphan{Name: name, Bytes: dirSize(path)}
		if !dryRun {
			if err := root.RemoveAll(name); err != nil {
				c.log.Warn("remove orphaned download", "path", name, "err", err)
				continue
			}
		}
		out = append(out, o)
	}

	if !dryRun && len(out) > 0 {
		c.log.Info("removed orphaned downloads", "files", len(out))
	}
	return out, nil
}

func dirSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	if !info.IsDir() {
		return allocatedSize(path, info)
	}

	var total int64
	filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			total += allocatedSize(p, fi)
		}
		return nil
	})
	return total
}
