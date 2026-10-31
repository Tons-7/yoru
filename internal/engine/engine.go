package engine

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/time/rate"
	"yoru/internal/bt"
	"yoru/internal/bt/metainfo"
	"yoru/internal/bt/storage"

	"yoru/internal/torrent"
)

type Options struct {
	CacheDir string

	SessionDir string

	ListenPort  int
	PeerLimit   int
	UploadLimit int
	DisableUPnP bool

	AwaitLimits bool
}

type Engine struct {
	opts Options
	log  *slog.Logger

	listener net.Listener
	server   *http.Server
	client   *torrent.Client

	saveMu  sync.Mutex
	startMu sync.Mutex
	running bool

	mu        sync.Mutex
	cl        *bt.Client
	pieces    storage.PieceCompletion
	byID      map[int]*entry
	byHash    map[metainfo.Hash]*entry
	nextID    int
	inspected map[metainfo.Hash]*inspection

	known map[metainfo.Hash]map[string]remembered

	peers *ledger

	busy  bool
	names *layout

	doomed map[string]doomedFile

	secret string
	closed bool

	limits    torrent.Limits
	limitsSet bool
	downLimit *rate.Limiter

	bound   bindAddrs
	offline bool
	keepIDs map[metainfo.Hash]int

	carry       *sessionDoc
	resolve     func(adapter string) (bindAddrs, error)
	janitorOnce sync.Once

	unclean     bool
	uncleanOnce sync.Once

	crashChecked map[metainfo.Hash]bool

	configure func(*bt.ClientConfig)
	onNew     func(*bt.Torrent)
}

type entry struct {
	id       int
	t        *bt.Torrent
	trackers []string

	selected map[int]bool
	paused   bool

	verifying bool
	err       string

	writeErr   string
	writeErrAt time.Time

	lastBytes int64
	lastAt    time.Time
	mibps     float64

	waits map[*watched]readWait

	quiet bool

	aloneSince  time.Time
	regroupWait time.Duration
	regroups    int

	heads headcount
}

type inspection struct {
	t       *bt.Torrent
	expires time.Time
}

const inspectionTTL = 3 * time.Minute

func New(opts Options, log *slog.Logger) (*Engine, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	e := &Engine{
		opts:      opts,
		log:       log,
		listener:  l,
		byID:      map[int]*entry{},
		byHash:    map[metainfo.Hash]*entry{},
		nextID:    1,
		inspected: map[metainfo.Hash]*inspection{},
		known:     map[metainfo.Hash]map[string]remembered{},
		peers:     newLedger(),
		names:     newLayout(opts.CacheDir),
		limits:    torrent.Limits{Upload: torrent.UploadWhileDownloading},
		downLimit: rate.NewLimiter(rate.Inf, 256<<10),
		resolve:   resolveAdapter,
	}
	go e.watchNetwork()
	e.secret = torrent.NewSecret()
	e.server = &http.Server{Handler: e.guard(l.Addr().String(), e.routes()), ReadHeaderTimeout: 10 * time.Second}
	go e.server.Serve(l)
	e.client = torrent.NewClient("http://" + l.Addr().String()).WithEngine(e).WithSecret(e.secret)
	return e, nil
}

func (e *Engine) Client() *torrent.Client { return e.client }

func (e *Engine) Addr() string { return e.listener.Addr().String() }

func (e *Engine) Ensure(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	e.startMu.Lock()
	defer e.startMu.Unlock()
	if e.running {
		return nil
	}
	if err := e.start(); err != nil {
		return fmt.Errorf("%w: %v", torrent.ErrUnavailable, err)
	}
	e.running = true
	return nil
}

func (e *Engine) start() error {
	e.mu.Lock()
	closed, waiting := e.closed, e.opts.AwaitLimits && !e.limitsSet
	e.mu.Unlock()
	if closed {
		return errors.New("engine stopped")
	}
	if waiting {
		return errors.New("waiting for settings to be read")
	}
	bind, err := e.binding()
	if err != nil {
		return err
	}
	for _, dir := range []string{e.opts.CacheDir, e.opts.SessionDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	e.uncleanOnce.Do(func() {
		marker := filepath.Join(e.opts.SessionDir, runningMarker)
		_, err := os.Stat(marker)
		e.unclean = err == nil
		if e.unclean {
			e.log.Warn("the last run did not stop cleanly; unfinished downloads are checked again")
		}
		os.WriteFile(marker, nil, 0o644)
	})
	pieces, reset, err := storage.NewBoltPieceCompletionRecovering(e.opts.SessionDir)
	if err != nil {
		return fmt.Errorf("piece completion: %w", err)
	}
	if reset {
		e.log.Warn("piece completion was damaged; moved aside, every download's data is checked again")
	}

	port := e.opts.ListenPort
	if port == 0 {
		port = 4240
	}
	newClient := func(port int) (*bt.Client, error) {
		cfg := e.clientConfig(pieces, port)
		applyBinding(cfg, bind)
		return bt.NewClient(cfg)
	}
	cl, err := newClient(port)
	if err != nil {
		e.log.Warn("peer port unavailable; using a random one", "port", port, "err", err)
		for range 5 {
			if cl, err = newClient(freePort()); err == nil {
				break
			}
		}
	}
	if err != nil {
		pieces.Close()
		return err
	}

	e.mu.Lock()
	e.keepIDs = make(map[metainfo.Hash]int, len(e.byHash))
	for hash, en := range e.byHash {
		e.keepIDs[hash] = en.id
	}
	for hash := range e.inspected {
		if _, managed := e.byHash[hash]; !managed {
			e.names.forget(hash)
			delete(e.known, hash)
		}
	}
	clear(e.byID)
	clear(e.byHash)
	clear(e.inspected)
	e.cl, e.pieces, e.bound, e.offline = cl, pieces, bind, false
	limits := e.limits
	e.mu.Unlock()
	cl.SetUploads(limits.Upload != torrent.UploadNever, true)

	if err := e.restore(); err != nil {
		e.log.Warn("restore torrents", "err", err)
	}
	e.mu.Lock()
	e.keepIDs = nil
	e.mu.Unlock()
	e.janitorOnce.Do(func() { go e.janitor() })
	e.log.Info("torrent engine started", "peer_port", cl.LocalPort(), "api", e.Addr(), "network", cmp.Or(bind.String(), "any"))
	return nil
}

var listenHost func(network string) string

func freePort() int {
	host := ""
	if listenHost != nil {
		host = listenHost("udp4")
	}
	for range 20 {
		pc, err := net.ListenPacket("udp", net.JoinHostPort(host, "0"))
		if err != nil {
			continue
		}
		port := pc.LocalAddr().(*net.UDPAddr).Port
		l, err := net.Listen("tcp", net.JoinHostPort(host, fmt.Sprint(port)))
		pc.Close()
		if err == nil {
			l.Close()
			return port
		}
	}
	return 0
}

func (e *Engine) clientConfig(pieces storage.PieceCompletion, port int) *bt.ClientConfig {
	cfg := bt.NewDefaultClientConfig()
	if listenHost != nil {
		cfg.ListenHost = listenHost
	}
	cfg.DataDir = e.opts.CacheDir
	cfg.DefaultStorage = files{base: e.opts.CacheDir, pieces: pieces, names: e.names, vanished: e.vanished}
	cfg.ListenPort = port
	if e.opts.PeerLimit > 0 {
		cfg.EstablishedConnsPerTorrent = e.opts.PeerLimit
	}
	if e.opts.UploadLimit > 0 {
		cfg.UploadRateLimiter = rate.NewLimiter(rate.Limit(e.opts.UploadLimit), 256<<10)
	}
	cfg.NoDefaultPortForwarding = e.opts.DisableUPnP
	cfg.UpnpDiscovered = func(routers int) {
		if routers == 0 {
			e.log.Info("no router answered for port forwarding; peers reach Yoru only if the peer port is forwarded", "port", port)
		}
	}
	cfg.UpnpMapped = func(proto string, internal, external int, err error) {
		if err != nil {
			e.log.Info("the router refused to forward the peer port", "proto", proto, "port", internal, "err", err)
			return
		}
		e.log.Info("the router forwards the peer port", "proto", proto, "port", internal, "external", external)
	}
	cfg.Seed = true
	e.mu.Lock()
	cfg.NoUpload = e.limits.Upload == torrent.UploadNever
	e.mu.Unlock()
	cfg.StopSeedingWhenComplete = true
	cfg.DownloadRateLimiter = e.downLimit
	cfg.Slogger = slog.New(warnOnly{e.log.Handler()})
	e.peers.hook(cfg)
	if e.configure != nil {
		e.configure(cfg)
	}
	return cfg
}

func (e *Engine) SetLimits(l torrent.Limits) {
	if l.DownloadBytes > 0 {
		e.downLimit.SetLimit(rate.Limit(l.DownloadBytes))
	} else {
		e.downLimit.SetLimit(rate.Inf)
	}
	e.mu.Lock()
	moved := e.limits.Adapter != l.Adapter
	e.limits = l
	e.limitsSet = true
	cl, offline := e.cl, e.offline
	e.mu.Unlock()
	if cl != nil {
		cl.SetUploads(l.Upload != torrent.UploadNever, true)
	}
	if moved && (cl != nil || offline) {
		go e.rebind("the network for torrents changed")
	}
}

func (e *Engine) Stop() {
	e.startMu.Lock()
	defer e.startMu.Unlock()
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return
	}
	e.closed = true
	cl, pieces := e.cl, e.pieces
	e.mu.Unlock()

	shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	e.server.Shutdown(shutdown)
	cancel()
	if cl != nil {
		e.writeSession(true)
		cl.Close()
	}
	if pieces != nil {
		pieces.Close()
	}
	os.Remove(filepath.Join(e.opts.SessionDir, runningMarker))
}

const runningMarker = "engine.running"

var janitorEvery = 5 * time.Second

func (e *Engine) janitor() {
	tick := time.NewTicker(janitorEvery)
	defer tick.Stop()
	for range tick.C {
		e.reap(time.Now())
		e.mu.Lock()
		if e.closed {
			e.mu.Unlock()
			return
		}
		if e.offline {
			e.mu.Unlock()
			continue
		}
		now := time.Now()
		var looking []*bt.Torrent
		for hash, in := range e.inspected {
			if now.After(in.expires) {
				delete(e.inspected, hash)
				if _, managed := e.byHash[hash]; !managed {
					in.t.Drop()
					delete(e.known, hash)
					e.names.forget(hash)
				}
				continue
			}
			looking = append(looking, in.t)
		}
		entries := make([]*entry, 0, len(e.byID))
		for _, en := range e.byID {
			entries = append(entries, en)
		}
		e.mu.Unlock()
		for _, t := range looking {
			peers := connected(t)
			e.mu.Lock()
			e.remember(t.InfoHash(), peers, now)
			e.mu.Unlock()
		}
		for _, en := range entries {
			e.retryWrites(en, now)
			e.shed(en, now)
			e.rescue(en, now)
			e.regroup(en, now)
			e.census(en, now)
		}
		e.focus(entries, len(looking))
	}
}

func (e *Engine) shed(en *entry, now time.Time) {
	if en.t.Info() == nil || en.t.Stats().ActivePeers < 2 {
		return
	}
	for _, pc := range e.peers.silent(en.t.PeerConns(), now) {
		e.log.Info("dropped a peer that stopped sending", "name", en.t.Name(), "peer", addrOf(pc), "quietFor", shedAfter)
		pc.Close()
	}
}

func (e *Engine) rescue(en *entry, now time.Time) {
	t := en.t
	if t.Info() == nil {
		return
	}
	type stuck struct {
		w    *watched
		wait readWait
	}
	var waiting []stuck
	e.mu.Lock()
	for w, wait := range en.waits {
		if now.Sub(wait.since) >= rescueAfter {
			waiting = append(waiting, stuck{w, wait})
		}
	}
	e.mu.Unlock()
	for _, s := range waiting {
		if t.Piece(s.wait.piece).State().Complete {
			continue
		}
		stale, holders := e.peers.holding(t.PeerConns(), s.wait.piece, rescueAfter, now)
		if t.Stats().ConnectedSeeders < 2 {
			stale = nil
		}
		for _, pc := range stale {
			e.log.Info("dropped a peer holding the piece a stream waits on", "name", t.Name(), "piece", s.wait.piece,
				"peer", addrOf(pc), "waited", now.Sub(s.wait.since).Round(time.Second))
			pc.Close()
		}
		if holders > 0 {
			continue
		}
		t.UpdateRequests()
		if s.wait.reported {
			continue
		}
		st := t.Stats()
		e.log.Info("a stream waits on a piece no peer was asked for", "name", t.Name(), "piece", s.wait.piece,
			"waited", now.Sub(s.wait.since).Round(time.Second), "peers", st.ActivePeers, "seeders", st.ConnectedSeeders)
		e.mu.Lock()
		if cur, ok := en.waits[s.w]; ok && cur.since.Equal(s.wait.since) {
			cur.reported = true
			en.waits[s.w] = cur
		}
		e.mu.Unlock()
	}
}

func (e *Engine) focus(entries []*entry, inspecting int) {
	busy := inspecting > 0
	e.mu.Lock()
	for _, en := range entries {
		if !en.paused && (en.t.Info() == nil || wantsData(en)) {
			busy = true
		}
	}
	e.busy = busy
	e.mu.Unlock()
	for _, en := range entries {
		e.quieten(en)
	}
}

func (e *Engine) refocus() {
	e.mu.Lock()
	entries := make([]*entry, 0, len(e.byID))
	for _, en := range e.byID {
		entries = append(entries, en)
	}
	looking := len(e.inspected)
	e.mu.Unlock()
	e.focus(entries, looking)
}

func (e *Engine) quieten(en *entry) {
	if en.t.Info() == nil {
		return
	}
	e.mu.Lock()
	quiet := en.paused || (e.busy && !wantsData(en))
	changed := quiet != en.quiet
	en.quiet = quiet
	e.mu.Unlock()
	if !changed {
		return
	}
	if quiet {
		en.t.DisallowDataUpload()
	} else {
		en.t.AllowDataUpload()
	}
}

func addrOf(pc *bt.PeerConn) string {
	if pc.RemoteAddr == nil {
		return ""
	}
	return pc.RemoteAddr.String()
}

func wantsData(en *entry) bool {
	for i, f := range en.t.Files() {
		if en.selected[i] && f.BytesCompleted() < f.Length() {
			return true
		}
	}
	return false
}

type warnOnly struct{ slog.Handler }

func (w warnOnly) Enabled(ctx context.Context, l slog.Level) bool {
	return l >= slog.LevelWarn && w.Handler.Enabled(ctx, l)
}

func (w warnOnly) WithAttrs(a []slog.Attr) slog.Handler { return warnOnly{w.Handler.WithAttrs(a)} }
func (w warnOnly) WithGroup(n string) slog.Handler      { return warnOnly{w.Handler.WithGroup(n)} }

var errNotFound = errors.New("torrent not found")

func (e *Engine) entryByID(id int) (*entry, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	en, ok := e.byID[id]
	if !ok {
		return nil, errNotFound
	}
	return en, nil
}

func sessionFile(dir string) string { return filepath.Join(dir, "session.json") }

func (e *Engine) added(t *bt.Torrent) {
	if e.onNew != nil {
		e.onNew(t)
	}
}
