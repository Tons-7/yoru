package library

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"yoru/internal/parse"
	"yoru/internal/player"
	"yoru/internal/score"
	"yoru/internal/sound"
	"yoru/internal/store"
	"yoru/internal/torrent"
	"yoru/internal/transcode"
)

type Player interface {
	Play(ctx context.Context, opts player.Options) error
	Events() <-chan player.Event
	Running() bool
	Stop()
}

type Playback struct {
	store   *store.Store
	finder  *Finder
	torrent *torrent.Client
	player  Player

	players  map[string]Player
	sync     *Sync
	enricher *Enricher
	prefetch *Prefetcher
	cache    *Cache
	prober   Prober

	relations *Relations

	presence *Presence
	cacheDir string
	log      *slog.Logger

	trackGen atomic.Uint64

	yieldMu  sync.Mutex
	yielded  []string
	starting int

	stageMu sync.Mutex
	stages  map[string]Stage
}

type Prober interface {
	Probe(ctx context.Context, source string) (*transcode.MediaInfo, error)
}

const probeDeadline = 12 * time.Second

const relationsDeadline = 10 * time.Second

func NewPlayback(s *store.Store, f *Finder, tc *torrent.Client, p Player, cacheDir string, log *slog.Logger) *Playback {
	return &Playback{store: s, finder: f, torrent: tc, player: p, cacheDir: cacheDir, log: log}
}

func (p *Playback) WithSync(s *Sync) *Playback             { p.sync = s; return p }
func (p *Playback) WithEnricher(e *Enricher) *Playback     { p.enricher = e; return p }
func (p *Playback) WithPrefetcher(f *Prefetcher) *Playback { p.prefetch = f; return p }
func (p *Playback) WithCache(c *Cache) *Playback           { p.cache = c; return p }
func (p *Playback) WithProber(pr Prober) *Playback         { p.prober = pr; return p }
func (p *Playback) WithRelations(r *Relations) *Playback   { p.relations = r; return p }
func (p *Playback) WithPresence(pr *Presence) *Playback    { p.presence = pr; return p }
func (p *Playback) WithPlayer(name string, pl Player) *Playback {
	if p.players == nil {
		p.players = map[string]Player{}
	}
	p.players[name] = pl
	return p
}

func (p *Playback) external(ctx context.Context) (string, Player) {
	prefs, err := p.store.Prefs(ctx, 0)
	if err == nil {
		if name := prefs.String("playback.player"); name != "" {
			if pl, ok := p.players[name]; ok {
				return name, pl
			}
		}
	}
	return "mpv", p.player
}

func (p *Playback) StopPlayers() {
	if p.player != nil {
		p.player.Stop()
	}
	for _, pl := range p.players {
		pl.Stop()
	}
}

type PlayRequest struct {
	AnimeID  int
	Episode  int
	Season   int
	InfoHash string
	External bool
	AllowRaw bool

	AudioChosen bool
	Prefs       score.Preferences
}

func (req PlayRequest) ruledOut(name, file string) bool {
	rel := parse.Parse(name)
	if strings.EqualFold(filepath.Ext(file), ".ts") {
		rel.Extension = "ts"
	}
	prefs := req.Prefs
	prefs.AllowRaw = req.AllowRaw
	if !req.AudioChosen {
		prefs.Audio = ""
	}
	return score.Unwanted(score.Candidate{Release: rel}, prefs)
}

type Session struct {
	AnimeID   int     `json:"animeId"`
	Episode   int     `json:"episode"`
	Title     string  `json:"title"`
	Source    string  `json:"source"`
	InfoHash  string  `json:"infoHash,omitempty"`
	TorrentID int     `json:"torrentId,omitempty"`
	FileIndex int     `json:"fileIndex"`
	StreamURL string  `json:"streamUrl"`
	LocalPath string  `json:"localPath,omitempty"`
	StartAt   float64 `json:"startAt"`
	Player    string  `json:"player"`

	Size int64 `json:"size,omitempty"`
}

func (p *Playback) Start(ctx context.Context, req PlayRequest) (*Session, error) {
	if req.InfoHash == "" {
		if session, ok, err := p.startLocal(ctx, req); err != nil {
			return nil, err
		} else if ok {
			return session, nil
		}
	}

	p.SetStage(req.AnimeID, req.Episode, Stage{Step: "search"})
	defer p.ClearStage(req.AnimeID, req.Episode)

	p.yieldMu.Lock()
	p.starting++
	p.yieldMu.Unlock()
	started := false
	defer func() {
		p.yieldMu.Lock()
		p.starting--
		p.yieldMu.Unlock()
		if !started {
			p.resumeYielded(context.WithoutCancel(ctx))
		}
	}()
	p.yield(ctx, req.AnimeID, req.Episode)

	if req.InfoHash == "" {
		p.prefetch.AwaitPrepare(ctx, req.AnimeID, req.Episode)
	}

	if req.InfoHash == "" {
		if session, ok, err := p.reattach(ctx, req); err != nil {
			return nil, err
		} else if ok {
			started = true
			return session, nil
		}
	}

	release, added, live, file, fileIndex, err := p.attach(ctx, req)
	if err != nil {
		return nil, err
	}

	if err := p.store.RecordTorrent(ctx, store.TorrentRecord{
		InfoHash:  release.Torrent.InfoHash,
		EngineID:  live.ID,
		Name:      added.Details.Name,
		TotalSize: file.Length,
		AnimeID:   req.AnimeID,
		EpKey:     epKey(req.Episode),
		FileIndex: fileIndex,
		FilePath:  file.Name,
		Manual:    req.InfoHash != "",
	}); err != nil {
		p.log.Warn("record torrent", "err", err)
	}
	dropSuperseded(ctx, p.store, p.torrent, p.log, req.AnimeID, epKey(req.Episode))

	resume, _ := p.store.ResumeAt(ctx, req.AnimeID, epKey(req.Episode))
	session := &Session{
		AnimeID:   req.AnimeID,
		Episode:   req.Episode,
		Title:     release.Torrent.Title,
		InfoHash:  release.Torrent.InfoHash,
		TorrentID: live.ID,
		FileIndex: fileIndex,
		StreamURL: p.torrent.StreamURL(live.ID, fileIndex),
		Source:    "torrent",
		StartAt:   resume,
		Player:    "browser",
		Size:      file.Length,
	}

	if req.External {
		if err := p.launch(ctx, req, session); err != nil {
			return nil, err
		}
	}

	started = true
	p.prefetchAfter(req)
	return session, nil
}

func (p *Playback) EngineAddr() string {
	if p == nil || p.torrent == nil {
		return ""
	}
	return p.torrent.Addr()
}

func (p *Playback) prefetchAfter(req PlayRequest) {
	p.prefetch.Prepare(req.AnimeID, req.Episode, req.Season, req.Prefs)
	p.prefetch.Next(req.AnimeID, req.Episode, req.Season, req.Prefs)
}

func (p *Playback) startLocal(ctx context.Context, req PlayRequest) (*Session, bool, error) {
	f, ok := localCopy(ctx, p.store, p.log, req.AnimeID, req.Episode)
	if !ok {
		return nil, false, nil
	}

	resume, _ := p.store.ResumeAt(ctx, req.AnimeID, epKey(req.Episode))
	session := &Session{
		AnimeID:   req.AnimeID,
		Episode:   req.Episode,
		Title:     filepath.Base(f.Path),
		Source:    "local",
		StreamURL: fmt.Sprintf("/api/local/%d/stream", f.ID),
		LocalPath: f.Path,
		StartAt:   resume,
		Player:    "browser",
		Size:      f.Size,
	}

	if req.External {
		if err := p.launch(ctx, req, session); err != nil {
			return nil, false, err
		}
	}

	p.log.Info("playing local file", "anime", req.AnimeID, "episode", req.Episode, "path", f.Path)
	p.prefetchAfter(req)
	return session, true, nil
}

func (p *Playback) reattach(ctx context.Context, req PlayRequest) (*Session, bool, error) {
	rec, ok, err := p.store.TorrentForEpisode(ctx, req.AnimeID, epKey(req.Episode))
	if err != nil {
		return nil, false, nil
	}
	playing := false
	if !ok {
		if rec, ok = p.prefetch.Fetching(req.AnimeID, req.Episode); !ok {
			return nil, false, nil
		}
		recorded := false
		defer func() {
			if !recorded && !playing {
				p.handBack(context.WithoutCancel(ctx), req, rec.InfoHash)
			}
		}()
		defer func() { recorded = p.store.TorrentClaimed(context.WithoutCancel(ctx), rec.InfoHash) }()
	}

	if !heldStands(ctx, p.store, p.finder, req.AnimeID, req.Episode, rec) {
		p.log.Warn("held release names another show, searching again",
			"anime", req.AnimeID, "episode", req.Episode, "release", rec.Name)
		return nil, false, nil
	}
	if req.ruledOut(rec.Name, rec.FilePath) {
		return nil, false, nil
	}
	if !rec.Manual && p.prefetch != nil && p.prefetch.ruledOut(rec.InfoHash) && !p.store.FileComplete(ctx, rec.InfoHash, rec.FileIndex) {
		p.log.Info("held release was found too slow, racing the others", "anime", req.AnimeID, "episode", req.Episode, "release", rec.Name)
		return nil, false, nil
	}

	live, err := p.torrent.Live(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("list downloads: %w", err)
	}
	id, held := live[strings.ToLower(rec.InfoHash)]
	if !held && p.store.TorrentFinishedOrKept(ctx, rec.InfoHash) {
		magnet := "magnet:?xt=urn:btih:" + strings.ToLower(rec.InfoHash)
		readd, cancel := context.WithTimeout(ctx, 20*time.Second)
		added, err := p.torrent.Add(readd, magnet, torrent.File{Name: rec.FilePath})
		cancel()
		if err == nil {
			p.log.Info("re-added a kept download the engine had lost", "anime", req.AnimeID, "episode", req.Episode)
			id, held = added.ID, true
		} else if now, lerr := p.torrent.Live(context.WithoutCancel(ctx)); lerr == nil {
			if stuck, ok := now[strings.ToLower(rec.InfoHash)]; ok {
				if ferr := p.torrent.Forget(context.WithoutCancel(ctx), stuck); ferr != nil {
					p.log.Warn("let go of a re-add that got no metadata", "torrent", stuck, "err", ferr)
				}
			}
		}
	}
	if !held {
		return nil, false, nil
	}
	if p.finder != nil && rec.FileIndex >= 0 && !rec.Manual {
		if detail, derr := p.torrent.Details(ctx, id); derr == nil && len(detail.Files) > 1 {
			scope := p.finder.PackScope(ctx, req.AnimeID, req.Episode, req.Season)
			numbers := fileNumbers(ctx, p.store, req.AnimeID, req.Episode)
			if f, i, ok := torrent.PickByTitle(detail.Files, req.Episode, numbers, scope); ok && i != rec.FileIndex {
				p.log.Info("held pack is numbered in another order; playing the file titled for this episode",
					"anime", req.AnimeID, "episode", req.Episode, "was", rec.FilePath, "now", f.Name)
				rec.FileIndex, rec.FilePath = i, f.Name
			}
		}
	}

	if p.prefetch != nil {
		p.prefetch.Unpause(ctx, rec)
	}
	if rec.FileIndex >= 0 {
		if err := p.torrent.IncludeFile(ctx, id, rec.FileIndex, true); err != nil {
			p.log.Debug("include the episode playing", "torrent", id, "err", err)
		}
	}
	if err := p.torrent.Start(ctx, id); err != nil {
		p.log.Debug("resume download", "torrent", id, "err", err)
	}
	waiting := func(peers int, bytes int64) {
		p.SetStage(req.AnimeID, req.Episode, Stage{Step: "data", Release: rec.Name, Peers: peers, Bytes: bytes})
	}
	waiting(0, 0)
	if err := p.warmed(ctx, id, rec.FileIndex, waiting, false); err != nil {
		if stats, serr := p.torrent.Stats(ctx, id); serr == nil && stats.Checking() {
			return nil, false, fmt.Errorf("episode %d is still being verified after a restart; try again in a minute", req.Episode)
		}
		p.log.Warn("held release is not delivering, searching again",
			"anime", req.AnimeID, "episode", req.Episode, "err", err)
		return nil, false, nil
	}

	if err := p.store.RecordTorrent(ctx, store.TorrentRecord{
		InfoHash: rec.InfoHash, EngineID: id, Name: rec.Name, TotalSize: rec.TotalSize,
		AnimeID: req.AnimeID, EpKey: epKey(req.Episode),
		FileIndex: rec.FileIndex, FilePath: rec.FilePath, Manual: rec.Manual,
	}); err != nil {
		p.log.Warn("record torrent", "err", err)
	}
	playing = true

	resume, _ := p.store.ResumeAt(ctx, req.AnimeID, epKey(req.Episode))
	session := &Session{
		AnimeID:   req.AnimeID,
		Episode:   req.Episode,
		Title:     rec.Name,
		InfoHash:  rec.InfoHash,
		TorrentID: id,
		FileIndex: rec.FileIndex,
		StreamURL: p.torrent.StreamURL(id, rec.FileIndex),
		Source:    "torrent",
		StartAt:   resume,
		Player:    "browser",
		Size:      rec.TotalSize,
	}

	if req.External {
		if err := p.launch(ctx, req, session); err != nil {
			return nil, true, err
		}
	}

	p.log.Info("resumed the release already held", "anime", req.AnimeID, "episode", req.Episode)
	p.prefetchAfter(req)
	return session, true, nil
}

var peerProbeDeadline = 8 * time.Second

const engineReadyDeadline = 4 * time.Minute

var (
	raceStagger  = 6 * time.Second
	raceWidth    = 3
	racePatience = 30 * time.Second
)

func (p *Playback) warmed(ctx context.Context, id, fileIndex int, waiting func(peers int, bytes int64), picked bool) error {
	if err := p.torrent.WaitLive(ctx, id, engineReadyDeadline); err != nil {
		return err
	}

	warm, cancel := context.WithTimeout(ctx, peerProbeDeadline)
	err := p.torrent.PrewarmHead(warm, id, fileIndex, 2<<20)
	cancel()
	if err == nil || ctx.Err() != nil {
		return err
	}

	stats, statErr := p.torrent.Stats(ctx, id)
	if statErr != nil {
		return err
	}

	if !picked && stats.Received() == 0 && stats.Peers() == 0 {
		return fmt.Errorf("no peers after %s", peerProbeDeadline)
	}

	p.log.Info("release is slow, waiting",
		"torrent", id, "bytes", stats.Received(), "peers", stats.Peers())
	stall := headStall
	if picked {
		stall = pickedStall
	}
	return p.awaitHead(ctx, id, fileIndex, stats.Received(), waiting, stall)
}

var (
	headPoll    = 3 * time.Second
	headStall   = 45 * time.Second
	headCap     = 6 * time.Minute
	pickedStall = 3 * time.Minute
)

func (p *Playback) awaitHead(
	ctx context.Context, id, fileIndex int, seen int64, waiting func(peers int, bytes int64), stall time.Duration,
) error {
	read, stop := context.WithTimeout(ctx, headCap)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- p.torrent.PrewarmHead(read, id, fileIndex, 2<<20) }()

	tick := time.NewTicker(headPoll)
	defer tick.Stop()
	moved := time.Now()
	for {
		select {
		case err := <-done:
			return err
		case <-tick.C:
		}
		stats, err := p.torrent.Stats(ctx, id)
		if err != nil {
			continue
		}
		if got := stats.Received(); got > seen {
			seen, moved = got, time.Now()
		}
		if waiting != nil {
			waiting(stats.Peers(), seen)
		}
		if time.Since(moved) > stall {
			stop()
			<-done
			return fmt.Errorf("stalled: nothing arrived for %s", stall)
		}
	}
}

const inspectAhead = 3

type inspection struct {
	torrent *torrent.Torrent
	err     error
}

func (p *Playback) inspect(ctx context.Context, picked bool, magnet string) (*torrent.Torrent, error) {
	if picked {
		return p.torrent.InspectPicked(ctx, magnet)
	}
	return p.torrent.Inspect(ctx, magnet)
}

func (p *Playback) inspectAhead(ctx context.Context, picked bool, candidates []score.Result) []chan inspection {
	pending := make([]chan inspection, len(candidates))
	for i := 0; i < len(candidates) && i < inspectAhead; i++ {
		pending[i] = make(chan inspection, 1)
		go func(i int, magnet string) {
			got, err := p.inspect(ctx, picked, magnet)
			pending[i] <- inspection{torrent: got, err: err}
		}(i, candidates[i].Torrent.Magnet())
	}
	return pending
}

func (p *Playback) awaitInspect(
	ctx context.Context, pending []chan inspection, i int, picked bool, magnet string,
) (*torrent.Torrent, error) {
	if i >= len(pending) || pending[i] == nil {
		return p.inspect(ctx, picked, magnet)
	}
	select {
	case got := <-pending[i]:
		return got.torrent, got.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *Playback) attach(ctx context.Context, req PlayRequest) (
	score.Result, *torrent.Torrent, *torrent.Torrent, torrent.File, int, error,
) {
	var failed string
	if p.prefetch != nil && req.InfoHash == "" {
		if rel, ok := p.prefetch.TakePrepared(req.AnimeID, req.Episode); ok &&
			!req.ruledOut(rel.Torrent.Title, rel.Torrent.Title) && !p.prefetch.ruledOut(rel.Torrent.InfoHash) {
			if r, a, l, f, fi, err := p.attachFrom(ctx, req, []score.Result{rel}); err == nil {
				return r, a, l, f, fi, nil
			}
			failed = rel.Torrent.InfoHash
			p.log.Info("resolved release did not deliver, searching",
				"anime", req.AnimeID, "episode", req.Episode)
		}
	}

	candidates, err := p.candidates(ctx, req)
	if err != nil {
		return score.Result{}, nil, nil, torrent.File{}, 0, err
	}
	if p.prefetch != nil && req.InfoHash == "" {
		ruled := func(r score.Result) bool { return p.prefetch.ruledOut(r.Torrent.InfoHash) }
		for moved := true; moved; {
			moved = false
			for i := 0; i+1 < len(candidates); i++ {
				a, b := candidates[i], candidates[i+1]
				if ruled(a) && !ruled(b) && a.Release.Resolution == b.Release.Resolution {
					candidates[i], candidates[i+1] = b, a
					moved = true
				}
			}
		}
	}
	if failed != "" {
		candidates = slices.DeleteFunc(candidates, func(r score.Result) bool {
			return strings.EqualFold(r.Torrent.InfoHash, failed)
		})
		if len(candidates) == 0 {
			return score.Result{}, nil, nil, torrent.File{}, 0,
				fmt.Errorf("the release found for episode %d is not delivering", req.Episode)
		}
	}
	return p.attachFrom(ctx, req, candidates)
}

func (p *Playback) attachFrom(ctx context.Context, req PlayRequest, candidates []score.Result) (
	score.Result, *torrent.Torrent, *torrent.Torrent, torrent.File, int, error,
) {
	held, err := p.torrent.Live(ctx)
	if err != nil {
		return score.Result{}, nil, nil, torrent.File{}, 0, fmt.Errorf("list downloads: %w", err)
	}

	pending := p.inspectAhead(ctx, req.InfoHash != "", candidates)
	return p.race(ctx, req, candidates, pending, held)
}

type attempt struct {
	index       int
	release     score.Result
	added, live *torrent.Torrent
	file        torrent.File
	fileIndex   int
	alreadyHeld bool
	slow        bool
	err         error
}

func (p *Playback) race(
	ctx context.Context, req PlayRequest, candidates []score.Result,
	pending []chan inspection, held map[string]int,
) (score.Result, *torrent.Torrent, *torrent.Torrent, torrent.File, int, error) {
	var zero score.Result
	stagger := raceStagger
	n := len(candidates)

	raceCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan attempt)
	slots := make(chan struct{}, raceWidth)
	failed := make(chan struct{}, n)
	go func() {
		for i := range n {
			select {
			case <-raceCtx.Done():
				return
			case slots <- struct{}{}:
			}
			go func(i int) {
				defer func() { <-slots }()
				a := p.tryCandidate(raceCtx, req, candidates[i], pending, held, i, n == 1)
				if a.err != nil {
					failed <- struct{}{}
				}
				select {
				case results <- a:
				case <-raceCtx.Done():
					if a.err == nil && a.live != nil && !a.alreadyHeld {
						p.discard(context.WithoutCancel(ctx), a.live.ID, a.added.Details.InfoHash)
					}
				}
			}(i)
			select {
			case <-raceCtx.Done():
				return
			case <-failed:
			case <-time.After(stagger):
			}
		}
	}()

	outcomes := make([]*attempt, n)
	commit := func(i int) (score.Result, *torrent.Torrent, *torrent.Torrent, torrent.File, int, error) {
		cancel()
		go p.discardLosers(ctx, outcomes, i)
		o := outcomes[i]
		return o.release, o.added, o.live, o.file, o.fileIndex, nil
	}

	asSharp := func() (int, bool) {
	next:
		for i := range n {
			o := outcomes[i]
			if o == nil || o.err != nil {
				continue
			}
			for j := range i {
				if outcomes[j] == nil && lines(candidates[j].Release.Resolution) != lines(o.release.Release.Resolution) {
					continue next
				}
			}
			p.log.Info("a better release is slow, playing one that is ready", "title", o.release.Torrent.Title)
			return i, true
		}
		return 0, false
	}

	fasterReady := func(i int, waitForBetter bool) (int, bool) {
		res := lines(outcomes[i].release.Release.Resolution)
		for j := range n {
			o := outcomes[j]
			switch {
			case j == i:
			case o == nil:
				if waitForBetter && lines(candidates[j].Release.Resolution) == res {
					return 0, false
				}
			case o.err == nil && !o.slow && lines(o.release.Release.Resolution) == res:
				return j, true
			}
		}
		return 0, false
	}

	slowReady := func(i int) int {
		n := 0
		for _, o := range outcomes {
			if o != nil && o.err == nil && o.slow && lines(o.release.Release.Resolution) == lines(outcomes[i].release.Release.Resolution) {
				n++
			}
		}
		return n
	}

	var lastErr error
	var patience, slowWait <-chan time.Time
	waited, slowWaited := false, false
	for received := 0; received < n; {
		var a attempt
		arrived := false
		select {
		case a = <-results:
			received++
			arrived = true
		case <-patience:
			waited, patience = true, nil
			if i, ok := asSharp(); ok {
				return commit(i)
			}
			continue
		case <-slowWait:
			slowWaited, slowWait = true, nil
		case <-ctx.Done():
			go p.discardLosers(ctx, outcomes, -1)
			return zero, nil, nil, torrent.File{}, 0, ctx.Err()
		}
		if arrived {
			outcomes[a.index] = &a
			if a.err != nil {
				lastErr = a.err
			} else if patience == nil && !waited {
				patience = time.After(racePatience)
			}
		}
		for i := range n {
			if outcomes[i] == nil {
				break
			}
			if outcomes[i].err != nil {
				continue
			}
			if outcomes[i].slow {
				if j, ok := fasterReady(i, !slowWaited); ok {
					p.log.Info("the better release is slow, playing a faster one", "title", outcomes[j].release.Torrent.Title)
					return commit(j)
				}
				if !slowWaited {
					if slowReady(i) >= 2 {
						p.log.Info("every ready release is slow, the connection is the limit; playing the best")
						return commit(i)
					}
					if slowWait == nil {
						slowWait = time.After(slowPatience)
					}
					break
				}
			}
			return commit(i)
		}
		if waited {
			if i, ok := asSharp(); ok {
				return commit(i)
			}
		}
	}
	for i := range n {
		if outcomes[i] != nil && outcomes[i].err == nil {
			return commit(i)
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no playable release for episode %d", req.Episode)
	}
	return zero, nil, nil, torrent.File{}, 0, fmt.Errorf(
		"tried %d release(s) for episode %d without one that would download: %w",
		n, req.Episode, lastErr)
}

func lines(resolution string) int {
	n, _ := strconv.Atoi(strings.TrimSuffix(strings.ToLower(resolution), "p"))
	return n
}

func (p *Playback) discardLosers(ctx context.Context, outcomes []*attempt, winner int) {
	clean := context.WithoutCancel(ctx)
	for _, a := range outcomes {
		if a != nil && a.index != winner && a.err == nil && a.live != nil && !a.alreadyHeld {
			p.discard(clean, a.live.ID, a.added.Details.InfoHash)
		}
	}
}

func (p *Playback) tryCandidate(
	ctx context.Context, req PlayRequest, release score.Result,
	pending []chan inspection, held map[string]int, index int, alone bool,
) attempt {
	fail := func(err error) attempt {
		p.dropStage(req.AnimeID, req.Episode, index)
		return attempt{index: index, err: err}
	}
	title := release.Torrent.Title

	p.setStage(req.AnimeID, req.Episode, index, Stage{Step: "files", Release: title})
	picked := req.InfoHash != ""
	added, err := p.awaitInspect(ctx, pending, index, picked, release.Torrent.Magnet())
	if err != nil {
		p.log.Warn("release unusable, trying the next", "title", release.Torrent.Title, "err", err)
		return fail(fmt.Errorf("inspect torrent: %w", err))
	}
	_, alreadyHeld := held[strings.ToLower(added.Details.InfoHash)]

	file, fileIndex, ok := pickFile(added.Details.Files, release)
	if !ok {
		p.log.Warn("episode missing from release", "title", added.Details.Name)
		return fail(fmt.Errorf("episode %d not found inside %q", req.Episode, added.Details.Name))
	}

	live, err := p.torrent.Add(ctx, release.Torrent.Magnet(), file)
	if err != nil {
		return fail(fmt.Errorf("add torrent: %w", err))
	}
	if err := p.torrent.Start(ctx, live.ID); err != nil {
		p.log.Debug("resume download", "torrent", live.ID, "err", err)
	}

	waiting := func(peers int, bytes int64) {
		p.setStage(req.AnimeID, req.Episode, index, Stage{Step: "data", Release: title, Peers: peers, Bytes: bytes})
	}
	waiting(0, 0)
	err = nil
	if !picked && !alone {
		err = p.torrent.WaitLive(ctx, live.ID, engineReadyDeadline)
	}
	started := time.Now()
	if err == nil {
		err = p.warmed(ctx, live.ID, fileIndex, waiting, picked)
	}
	if err != nil {
		if ctx.Err() != nil {
			p.log.Info("release stopped, another one won", "title", release.Torrent.Title)
		} else {
			p.log.Warn("release is not delivering, trying the next",
				"title", release.Torrent.Title, "seeders", release.Torrent.Seeders, "err", err)
		}
		if !alreadyHeld {
			p.discard(context.WithoutCancel(ctx), live.ID, added.Details.InfoHash)
		}
		return fail(fmt.Errorf("no data from the swarm: %w", err))
	}
	slow := false
	if !picked && !alone {
		rate, need := p.pace(ctx, live.ID, file, release.RuntimeMinutes, time.Since(started))
		slow = rate < need
		if slow {
			p.setStage(req.AnimeID, req.Episode, index, Stage{Step: "slow", Release: title})
			p.log.Info("release downloads slower than it plays",
				"title", title, "kbps", int(rate/1024), "need", int(need/1024), "seeders", release.Torrent.Seeders)
		}
	}
	return attempt{
		index: index, release: release, added: added, live: live,
		file: file, fileIndex: fileIndex, alreadyHeld: alreadyHeld, slow: slow,
	}
}

var (
	paceSample   = 3 * time.Second
	paceFactor   = 1.0
	slowPatience = 10 * time.Second
	fewPeers     = 4
)

func (p *Playback) pace(ctx context.Context, id int, file torrent.File, runtime int, took time.Duration) (rate, need float64) {
	if runtime <= 0 {
		runtime = 24
	}
	need = paceFactor * float64(file.Length) / float64(runtime*60)
	first, err := p.torrent.Stats(ctx, id)
	if err != nil || first.Finished || first.Peers() >= fewPeers {
		return need, need
	}
	if took > 0 {
		rate = float64(2<<20) / took.Seconds()
	}
	if first.Live != nil {
		rate = max(rate, first.Live.BytesPerSecond())
	}
	if rate >= need {
		return rate, need
	}
	select {
	case <-ctx.Done():
		return need, need
	case <-time.After(paceSample):
	}
	second, err := p.torrent.Stats(ctx, id)
	if err != nil || second.Finished || second.Peers() >= fewPeers {
		return need, need
	}
	return max(rate, float64(second.Received()-first.Received())/paceSample.Seconds()), need
}

func (p *Playback) Suspend(ctx context.Context, animeID, episode int) {
	if p.torrent == nil || animeID == 0 || episode <= 0 {
		return
	}
	defer p.autoDelete(animeID)
	defer p.resumeYielded(context.WithoutCancel(ctx))

	hash, index, err := p.store.CachedFile(ctx, animeID, epKey(episode))
	if err != nil || hash == "" {
		return
	}

	if err := p.store.PinCache(ctx, hash, index, false); err != nil {
		p.log.Warn("unpin", "anime", animeID, "episode", episode, "err", err)
	}

	live, err := p.torrent.Live(ctx)
	if err != nil {
		return
	}
	id, ok := live[strings.ToLower(hash)]
	if !ok {
		return
	}

	if stats, err := p.torrent.Stats(ctx, id); err == nil && stats.Finished {
		return
	}

	if global, err := p.store.Prefs(ctx, 0); err == nil && global.Bool("cache.prefetch_full") {
		p.log.Info("download left running to finish the episode",
			"anime", animeID, "episode", episode)
		return
	}
	if err := p.torrent.Pause(ctx, id); err != nil {
		p.log.Debug("pause download", "torrent", id, "err", err)
		return
	}
	p.log.Info("download paused, nothing watching it", "anime", animeID, "episode", episode)
}

func (p *Playback) yield(ctx context.Context, animeID, episode int) {
	if p.torrent == nil || !p.yieldWanted(ctx, animeID, episode) {
		return
	}
	entries, err := p.store.CacheEntries(ctx)
	if err != nil {
		return
	}
	live, err := p.torrent.Live(ctx)
	if err != nil {
		return
	}
	paused := map[string]bool{}
	for _, e := range entries {
		hash := strings.ToLower(e.InfoHash)
		if e.Complete || e.Pinned || e.Kept || e.AnimeID == nil || *e.AnimeID == animeID || paused[hash] {
			continue
		}
		id, ok := live[hash]
		if !ok {
			continue
		}
		if stats, err := p.torrent.Stats(ctx, id); err != nil || stats.Finished || stats.State == "paused" {
			continue
		}
		if err := p.torrent.Pause(ctx, id); err != nil {
			continue
		}
		paused[hash] = true
		p.log.Info("paused a background download for the episode starting", "torrent", id, "title", e.Title)
	}
	p.yieldMu.Lock()
	for hash := range paused {
		p.yielded = append(p.yielded, hash)
	}
	p.yieldMu.Unlock()
}

func (p *Playback) yieldWanted(ctx context.Context, animeID, episode int) bool {
	if prefs, err := p.store.Prefs(ctx, 0); err == nil && prefs.String("downloads.while_watching") == "continue" {
		return false
	}
	hash, index, _ := p.store.CachedFile(ctx, animeID, epKey(episode))
	if hash == "" {
		return true
	}
	return !p.store.FileComplete(ctx, hash, index) && !(p.cache != nil && p.cache.Finished(ctx, hash))
}

func (p *Playback) ResumeYielded(ctx context.Context) { p.resumeYielded(ctx) }

func (p *Playback) resumeYielded(ctx context.Context) {
	p.yieldMu.Lock()
	busy := p.starting > 0
	p.yieldMu.Unlock()
	if busy {
		return
	}
	if entries, err := p.store.CacheEntries(ctx); err == nil {
		for _, e := range entries {
			if e.Pinned {
				return
			}
		}
	}
	p.yieldMu.Lock()
	hashes := p.yielded
	p.yielded = nil
	p.yieldMu.Unlock()
	if len(hashes) == 0 {
		return
	}
	live, err := p.torrent.Live(ctx)
	if err != nil {
		return
	}
	for _, hash := range hashes {
		if p.prefetch != nil && p.prefetch.pausedByUser(hash) {
			continue
		}
		if id, ok := live[hash]; ok {
			if err := p.torrent.Start(ctx, id); err != nil {
				p.log.Debug("resume background download", "torrent", id, "err", err)
			}
		}
	}
}

func (p *Playback) handBack(ctx context.Context, req PlayRequest, hash string) {
	if p.prefetch.HandBack(req.AnimeID, req.Episode, hash) {
		return
	}
	live, err := p.torrent.Live(ctx)
	if err != nil {
		return
	}
	if id, ok := live[strings.ToLower(hash)]; ok {
		discardUnclaimed(ctx, p.store, p.torrent, p.log, id, hash)
	}
}

func (p *Playback) discard(ctx context.Context, id int, hash string) {
	discardUnclaimed(ctx, p.store, p.torrent, p.log, id, hash)
}

func discardUnclaimed(ctx context.Context, st *store.Store, tc *torrent.Client, log *slog.Logger, id int, hash string) {
	if st.TorrentClaimed(ctx, hash) || st.TorrentFinishedOrKept(ctx, hash) {
		return
	}
	if err := tc.Delete(ctx, id); err != nil {
		log.Warn("remove abandoned release", "torrent", id, "err", err)
		return
	}
	if err := st.DropTorrentCache(ctx, strings.ToLower(hash)); err != nil {
		log.Warn("forget abandoned release", "torrent", id, "err", err)
	}
	log.Info("abandoned release removed", "torrent", id)
}

func (p *Playback) candidates(ctx context.Context, req PlayRequest) ([]score.Result, error) {
	req.Prefs.AllowRaw = req.AllowRaw

	found, err := p.finder.Find(ctx, Request{
		AnimeID: req.AnimeID,
		Episode: req.Episode,
		Season:  req.Season,
		Prefs:   req.Prefs,
	})
	if err != nil {
		return nil, err
	}

	if req.InfoHash != "" {
		for _, r := range found.Results {
			if r.Torrent.InfoHash == req.InfoHash {
				return []score.Result{r}, nil
			}
		}
		return nil, fmt.Errorf("release %s not found", req.InfoHash)
	}

	if found.Best == nil {
		aired, airsAt := p.store.EpisodeAired(ctx, req.AnimeID, req.Episode)
		return nil, noRelease(found, req, aired, airsAt)
	}

	out := []score.Result{*found.Best}
	for _, r := range found.Results {
		if len(out) >= maxAttempts {
			break
		}
		if r.Torrent.InfoHash != found.Best.Torrent.InfoHash && r.AutoPick {
			out = append(out, r)
		}
	}
	return out, nil
}

const maxAttempts = 4

type NoRelease struct {
	Episode  int
	Queries  int
	Found    int
	RawOnly  bool
	RawTitle string

	Unaired bool
	AirsAt  int64
	message string
}

func (e *NoRelease) Error() string { return e.message }

func airsIn(at int64) string {
	if at <= 0 {
		return ""
	}
	d := time.Until(time.Unix(at, 0))
	switch {
	case d <= 0:
		return ""
	case d < time.Hour:
		return fmt.Sprintf("; it airs in %d minutes", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("; it airs in %d hours", int(d.Hours()))
	}
	return fmt.Sprintf("; it airs in %d days", int(d.Hours()/24))
}

func noRelease(found Candidates, req PlayRequest, aired bool, airsAt int64) error {
	out := &NoRelease{
		Episode: req.Episode,
		Queries: len(found.Queries),
		Found:   len(found.Results),
		Unaired: !aired,
		AirsAt:  airsAt,
	}

	for _, r := range found.Results {
		if r.RejectedOnlyAsRaw() {
			out.RawOnly = true
			out.RawTitle = r.Torrent.Title
			break
		}
	}

	switch {
	case out.Unaired:
		out.message = fmt.Sprintf("episode %d has not aired yet%s", req.Episode, airsIn(airsAt))
	case out.RawOnly:
		out.message = fmt.Sprintf(
			"episode %d is out as a raw broadcast, but no subtitled release yet; "+
				"subs usually follow within a few hours", req.Episode)
	case out.Found == 0:
		out.message = fmt.Sprintf(
			"no release found for episode %d (the search covered %d title variant(s))",
			req.Episode, out.Queries)
	default:
		out.message = fmt.Sprintf(
			"found %d release(s) for episode %d, but none met your quality or audio preferences: "+
				"pick one manually or relax the settings", out.Found, req.Episode)
	}
	return out
}

func (p *Playback) launch(ctx context.Context, req PlayRequest, s *Session) error {
	opts := player.Options{
		URL:          s.StreamURL,
		Title:        s.Title,
		StartAt:      s.StartAt,
		Audio:        req.Prefs.Audio,
		SubLanguages: req.Prefs.SubLanguages,
	}
	if s.LocalPath != "" {
		opts.URL = s.LocalPath
	}

	if s.InfoHash != "" {
		if err := p.store.PinPlaying(ctx, req.AnimeID, epKey(req.Episode), s.InfoHash, s.FileIndex); err != nil {
			p.log.Warn("pin playing episode", "anime", req.AnimeID, "err", err)
		}
	}

	prefs, _ := p.store.Prefs(ctx, req.AnimeID)
	opts.Anime4K = prefs.Bool("playback.anime4k")
	opts.Anime4KMode = prefs.String("playback.anime4k_mode")
	opts.Anime4KSize = prefs.String("playback.anime4k_size")
	opts.Sound = sound.Options{
		Voice: prefs.String("playback.voice"), Night: prefs.Bool("playback.night"),
		Headphones: prefs.Bool("playback.headphones"), Mono: prefs.Bool("playback.mono"),
		Boost: prefs.String("playback.boost"),
	}
	opts.SubLook = prefs.String("subtitle.look")
	opts.SubRaise = prefs.Bool("subtitle.raise")
	opts.SoftwareDecode = !prefs.Bool("playback.mpv_hwdec")
	opts.Deband = prefs.Bool("playback.mpv_deband")
	opts.Soften = prefs.Bool("playback.soften_flashing")

	var ending float64
	if ranges, duration := p.skipRanges(ctx, req, opts.URL, prefs); len(ranges) > 0 {
		ending = player.EndingStart(ranges)
		opts.SkipRanges = p.autoSkip(ctx, req, prefs).Skipped(ranges)
		opts.AutoSkip = len(opts.SkipRanges) > 0

		path := filepath.Join(p.cacheDir, fmt.Sprintf("chapters-%d-%d.ini", req.AnimeID, req.Episode))
		if err := player.WriteChapters(path, ranges, duration); err == nil {
			opts.ChaptersFile = path
		}
	}

	name, pl := p.external(ctx)
	if pl == nil {
		return fmt.Errorf("no desktop player")
	}
	gen := p.trackGen.Add(1)
	if err := pl.Play(ctx, opts); err != nil {
		return fmt.Errorf("start %s: %w", name, err)
	}
	s.Player = name
	go p.track(pl, req.AnimeID, req.Episode, req.Season, gen, ending)
	return nil
}

func (p *Playback) skipRanges(ctx context.Context, req PlayRequest, source string, prefs store.Prefs) ([]player.SkipRange, float64) {
	var chapters []player.SkipRange
	var duration int

	if p.prober != nil && source != "" {
		probeCtx, cancel := context.WithTimeout(ctx, probeDeadline)
		info, err := p.prober.Probe(probeCtx, source)
		cancel()
		if err != nil {
			p.log.Debug("probe for skip times", "anime", req.AnimeID, "err", err)
		} else {
			duration = int(info.Duration)
			names := player.SkipNames(prefs.String("playback.skip_chapters"))
			for _, c := range info.Chapters {
				if kind := player.ChapterKind(c.Kind, c.Title, names); kind != "" {
					chapters = append(chapters, player.SkipRange{Kind: kind, Start: c.Start, End: c.End, Source: "chapters"})
				}
			}
		}
	}

	own, _ := p.store.OwnSkips(ctx, req.AnimeID, req.Episode)
	ranges := player.Merge(player.From(own, "marked"), chapters)
	if !player.HasPart(ranges, "op") || !player.HasPart(ranges, "ed") {
		if p.enricher != nil {
			if err := p.enricher.Skips(ctx, req.AnimeID, req.Episode, duration); err != nil {
				p.log.Warn("fetch skip times", "anime", req.AnimeID, "err", err)
			}
		}
		if stored, err := p.store.SkipRanges(ctx, req.AnimeID, req.Episode); err == nil {
			ranges = player.Merge(ranges, stored)
		}
	}
	return player.Merge(ranges, player.From(own, "found")), float64(duration)
}

func (p *Playback) autoSkip(ctx context.Context, req PlayRequest, prefs store.Prefs) player.AutoSkip {
	a := player.AutoSkip{
		Opening: prefs.SkipMode("playback.autoskip_op") == store.SkipAuto,
		Ending:  prefs.SkipMode("playback.autoskip_ed") == store.SkipAuto,
		Recap:   prefs.Bool("playback.autoskip_recap"), Preview: prefs.Bool("playback.autoskip_preview"),
	}
	if prefs.Bool("playback.skip_keep_bookends") {
		total, _ := p.store.EpisodeCount(ctx, req.AnimeID)
		if req.Episode == 1 || total > 0 && req.Episode == total && p.store.Finished(ctx, req.AnimeID) {
			a.Opening, a.Ending = false, false
		}
	}
	return a
}

func (p *Playback) track(pl Player, animeID, episode, season int, gen uint64, ending float64) {
	const saveEvery = 5 * time.Second

	var last time.Time
	var reported, positioned bool

	var played, lastPos, lastDur float64
	const maxStep = 5.0

	for ev := range pl.Events() {
		if p.trackGen.Load() != gen {
			return
		}
		switch ev.Kind {
		case player.EventPosition:
			positioned = true
			if step := ev.Position - lastPos; step > 0 && step <= maxStep {
				played += step
			}
			lastPos = ev.Position
			if ev.Duration > 0 {
				lastDur = ev.Duration
			}
			p.presence.Report(context.Background(), animeID, episode, ev.Position, lastDur, ev.Paused, false, true)

			if time.Since(last) < saveEvery {
				continue
			}
			last = time.Now()
			if p.save(animeID, episode, ev, played, ending) && !reported {
				reported = true
				p.watched(animeID, episode)
			}
			played = 0

		case player.EventEnd, player.EventExit:
			p.presence.Stop()
			if ev.Position == 0 {
				ev.Position = lastPos
			}
			if ev.Reason == "eof" && lastDur > 0 {
				ev.Position, ev.Duration = lastDur, lastDur
			}
			if positioned || ev.Position > 0 {
				if p.save(animeID, episode, ev, played, ending) && !reported {
					reported = true
					p.watched(animeID, episode)
				}
			}
			played = 0
			if !reported && !positioned && ev.Reason == "eof" {
				p.watched(animeID, episode)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			p.Suspend(ctx, animeID, episode)
			cancel()

			if ev.Kind == player.EventEnd && ev.Reason == "eof" {
				p.advance(animeID, episode, season)
			}
			return
		}
	}
}

func (p *Playback) advance(animeID, episode, season int) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	prefs, err := p.store.Prefs(ctx, animeID)
	if err != nil || !prefs.Bool("playback.autonext") {
		return
	}

	picks, _, err := p.store.PlayOrder(ctx, animeID, episode, 1)
	if err != nil || len(picks) == 0 {
		return
	}
	next := picks[0]
	if next.AnimeID != animeID {
		if prefs, err = p.store.Prefs(ctx, next.AnimeID); err != nil {
			return
		}
		season = 0
	}

	p.log.Info("advancing to next episode", "anime", next.AnimeID, "episode", next.Episode)

	if _, err := p.Start(ctx, PlayRequest{
		AnimeID:  next.AnimeID,
		Episode:  next.Episode,
		Season:   season,
		External: true,
		Prefs:    prefsFromSettings(prefs),
	}); err != nil {
		p.log.Warn("auto-advance", "anime", next.AnimeID, "episode", next.Episode, "err", err)
	}
}

func (p *Playback) watched(animeID, episode int) {
	if p.sync == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := p.sync.Watched(ctx, animeID, episode); err != nil {
		p.log.Warn("record watched", "anime", animeID, "episode", episode, "err", err)
	}
	p.autoDelete(animeID)
}

func (p *Playback) autoDelete(animeID int) {
	if p.cache == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := p.cache.AutoDelete(ctx, animeID); err != nil {
		p.log.Warn("auto-delete watched", "anime", animeID, "err", err)
	}
}

func (p *Playback) save(animeID, episode int, ev player.Event, played, ending float64) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	watched, err := p.store.SavePlayback(ctx, store.PlaybackState{
		AnimeID:  animeID,
		EpKey:    epKey(episode),
		Position: ev.Position,
		Duration: ev.Duration,
		Played:   played,
		Ending:   ending,
	})
	if err != nil {
		p.log.Warn("save playback", "err", err)
		return false
	}
	return watched
}

func epKey(episode int) string { return fmt.Sprint(episode) }
