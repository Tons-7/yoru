package main

//go:generate go run github.com/tc-hib/go-winres@v0.3.3 simply --arch amd64,arm64 --manifest none --icon ../../web/public/icon-512.png --out rsrc

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"yoru/internal/anilist"
	"yoru/internal/certs"
	"yoru/internal/config"
	"yoru/internal/corpus"
	"yoru/internal/db"
	"yoru/internal/deps"
	"yoru/internal/engine"
	"yoru/internal/indexer"
	"yoru/internal/jobs"
	"yoru/internal/library"
	"yoru/internal/mal"
	"yoru/internal/metadata"
	"yoru/internal/player"
	"yoru/internal/power"
	"yoru/internal/proc"
	"yoru/internal/push"
	"yoru/internal/score"
	"yoru/internal/server"
	"yoru/internal/shortcut"
	"yoru/internal/store"
	"yoru/internal/transcode"
	"yoru/internal/tray"
	"yoru/internal/tunnel"
	"yoru/internal/update"
)

func windowWanted() bool {
	if v := strings.ToLower(config.Env("NO_WINDOW")); v == "1" || v == "true" {
		return false
	}
	for _, arg := range os.Args[1:] {
		if arg == "--no-window" || arg == "-no-window" {
			return false
		}
	}
	return true
}

func startedInTray() bool {
	for _, arg := range os.Args[1:] {
		if arg == shortcut.TrayArg {
			return true
		}
	}
	return false
}

func interval(st *store.Store, key string, fallback time.Duration) time.Duration {
	prefs, err := st.Prefs(context.Background(), 0)
	if err != nil {
		return fallback
	}
	if secs := prefs.Int(key); secs >= 60 {
		return time.Duration(secs) * time.Second
	}
	return fallback
}

func newTorrentEngine(cfg config.Config, log *slog.Logger) (*engine.Engine, error) {
	upload := cfg.Torrent.UploadLimitBytes
	if upload == 0 {
		upload = 4 << 20
	}
	return engine.New(engine.Options{
		CacheDir:    cfg.CacheDir,
		SessionDir:  filepath.Join(cfg.CacheDir, library.EngineSessionDirName),
		ListenPort:  cfg.Torrent.PeerPort(),
		PeerLimit:   cfg.Torrent.PeerLimit,
		UploadLimit: upload,
		DisableUPnP: !cfg.Torrent.UPnPEnabled(),
		AwaitLimits: true,
	}, log)
}

func debounce(wait time.Duration, fn func()) func() {
	var pending atomic.Bool
	return func() {
		if pending.CompareAndSwap(false, true) {
			time.AfterFunc(wait, func() {
				pending.Store(false)
				fn()
			})
		}
	}
}

func parseStreamID(id string) (animeID, episode int, ok bool) {
	id = transcode.BaseID(id)
	dash := strings.LastIndexByte(id, '-')
	if dash <= 0 {
		return 0, 0, false
	}
	animeID, err := strconv.Atoi(id[:dash])
	if err != nil {
		return 0, 0, false
	}
	episode, err = strconv.Atoi(id[dash+1:])
	if err != nil {
		return 0, 0, false
	}
	return animeID, episode, true
}

func anilistClientID(ctx context.Context, st *store.Store, cfg config.Config) string {
	if id, _ := st.Setting(ctx, "anilist.client_id"); id != "" {
		return id
	}
	return cfg.AniList.ClientID
}

func newMAL(ctx context.Context, cfg config.Config, st *store.Store, log *slog.Logger) *mal.Client {
	id, _ := st.Setting(ctx, "mal.client_id")
	secret, _ := st.Setting(ctx, "mal.client_secret")
	if id == "" {
		id, secret = cfg.MAL.ClientID, cfg.MAL.ClientSecret
	}

	client := mal.New(log,
		mal.WithCredentials(id, secret),
		mal.OnToken(func(t mal.Token) {
			err := st.SetSettings(context.Background(), map[string]string{
				"mal.token":         t.Access,
				"mal.refresh.token": t.Refresh,
				"mal.expires_at":    strconv.FormatInt(t.Expires.Unix(), 10),
			})
			if err != nil {
				log.Warn("persist mal token", "err", err)
			}
		}))

	access, _ := st.Setting(ctx, "mal.token")
	if access == "" {
		if id != "" {
			log.Info("myanimelist not connected", "connect", "http://"+cfg.Addr+"/api/mal/auth/login")
		}
		return client
	}

	refresh, _ := st.Setting(ctx, "mal.refresh.token")
	expires, _ := st.SettingInt(ctx, "mal.expires_at")
	client.SetToken(mal.Token{
		Access:  access,
		Refresh: refresh,
		Expires: time.Unix(int64(expires), 0),
	})

	name, _ := st.Setting(ctx, "mal.user_name")
	log.Info("myanimelist connected", "user", name)
	return client
}

func main() {
	update.WaitFor(os.Args[1:], 30*time.Second)
	out := &logSink{}
	log := slog.New(slog.NewTextHandler(out, nil))
	if err := run(log, out); err != nil {
		log.Error("fatal", "err", err)
		out.keepEarly(config.DefaultDataDir())
		proc.Alert("Yoru could not start", err.Error())
		os.Exit(1)
	}
}

type logSink struct {
	mu     sync.Mutex
	file   *os.File
	early  bytes.Buffer
	closed bool
}

func (s *logSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	switch {
	case s.file != nil:
		s.file.Write(p)
	case !s.closed:
		s.early.Write(p)
	}
	s.mu.Unlock()
	os.Stdout.Write(p)
	return len(p), nil
}

func (s *logSink) open(dir string) {
	path := filepath.Join(dir, config.Name+".log")
	var f *os.File
	var err error
	if rerr := os.Rename(path, path+".1"); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
		f, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	} else {
		f, err = os.Create(path)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.early, s.closed = bytes.Buffer{}, true
		return
	}
	f.Write(s.early.Bytes())
	s.early = bytes.Buffer{}
	s.file = f
}

func (s *logSink) keepEarly(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file != nil || s.closed || s.early.Len() == 0 || os.MkdirAll(dir, 0o755) != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, config.Name+".log"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	f.Write(s.early.Bytes())
	f.Close()
	s.early.Reset()
}

func run(log *slog.Logger, out *logSink) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stopped := make(chan struct{})
	defer close(stopped)

	exe, _ := os.Executable()
	serving := false
	if exe != "" && update.Handover(os.Args[1:]) {
		defer func() {
			if serving {
				return
			}
			if err := update.Rollback(exe); err != nil {
				log.Error("restore the previous version", "err", err)
				return
			}
			log.Warn("this version failed to start; the previous one is back")
			if err := update.Relaunch(exe); err != nil {
				log.Error("start the previous version", "err", err)
			}
		}()
	}
	log.Info("Yoru", "version", update.Version)
	config.FinderPath()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	showRunning := func() error {
		if !yoruAnswers(cfg.LocalURL()) {
			return fmt.Errorf("Yoru is already running on this data folder, or another program holds %s", cfg.Addr)
		}
		log.Info("Yoru is already running; showing it", "url", cfg.LocalURL())
		if windowWanted() && !startedInTray() && !askToShow(cfg.LocalURL()) {
			config.OpenApp(ctx, cfg.LocalURL(), cfg.ProfileDir(), log)
		}
		return nil
	}
	unlock, err := proc.Lock(cfg.LockPath())
	if err != nil {
		return showRunning()
	}
	defer unlock()
	out.open(cfg.DataDir())
	claim, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return showRunning()
	}
	defer func() {
		if !serving {
			claim.Close()
		}
	}()

	if err := proc.KillChildrenOnExit(); err != nil {
		log.Warn("helpers may outlive an abrupt exit", "err", err)
	}
	warnSlowDisk(log, cfg.BinDir, cfg.CacheDir)

	conn, err := db.Open(cfg.DatabasePath())
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := conn.Migrate(); err != nil {
		return err
	}
	log.Info("database ready", "path", cfg.DatabasePath())

	st := store.New(conn)
	var alOpts []anilist.Option
	if override := config.Env("ANILIST_API"); override != "" {
		alOpts = append(alOpts, anilist.WithEndpoint(override))
	}
	al := anilist.New(log, alOpts...)
	al.SetArchive(st)

	token, err := st.EnsureSetting(ctx, "access.token", server.NewAccessToken())
	if err != nil {
		return err
	}

	switch token, _ := st.Setting(ctx, "anilist.token"); {
	case token != "":
		al.SetToken(token)
		name, _ := st.Setting(ctx, "anilist.user_name")
		if expires, _ := st.SettingInt(ctx, "anilist.expires_at"); expires > 0 {
			if left := time.Until(time.Unix(int64(expires), 0)); left < 30*24*time.Hour {
				log.Warn("anilist token expiring", "in", left.Round(time.Hour))
			}
		}
		log.Info("anilist connected", "user", name)
	case anilistClientID(ctx, st, cfg) == "":
		log.Warn("anilist not configured; search works, sync does not",
			"add", "Settings -> Trackers", "register", "https://anilist.co/settings/developer")
	default:
		log.Info("anilist not connected", "connect", "http://"+cfg.Addr+"/api/auth/login")
	}

	malClient := newMAL(ctx, cfg, st, log)

	fresh, err := push.NewKeys()
	if err != nil {
		return err
	}
	stored, err := st.EnsureSetting(ctx, "push.vapid_key", fresh.Encode())
	if err != nil {
		return err
	}
	var pusher *push.Sender
	if keys, err := push.DecodeKeys(stored); err != nil {
		log.Warn("phone alerts off", "err", err)
	} else {
		pusher = push.NewSender(keys, st, log)
		st.OnNotification(func(n store.Notification) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			if alert, ok := server.ReleaseAlert(ctx, st, n); ok {
				pusher.Send(ctx, alert)
			}
		})
	}

	sites := indexer.NewSites()
	siteList, saved, err := server.LoadSites(ctx, st, cfg)
	if err != nil {
		log.Warn("release sites saved in the app are unreadable; using config.toml's", "err", err)
	}
	for _, err := range sites.Set(siteList) {
		log.Warn("skipping a release site", "err", err)
	}
	if general, adult := sites.Counts(); general > 0 {
		log.Info("release sources", "sites", general, "adult", adult, "saved_in_app", saved)
	} else {
		log.Warn("no release sites yet; nothing can be searched", "add", "Settings › Downloads › Release sites")
	}
	if stray := cfg.StrayConfig(); stray != "" {
		log.Warn("config.toml.txt found; Yoru reads only config.toml", "path", stray)
	}
	finder := library.NewFinder(st, sites.General(), log).WithAdultIndexer(sites.Adult())
	enricher := library.NewEnricher(st, metadata.New(), log).WithAniList(al)
	ingester := library.NewIngester(st, corpus.NewFetcher(), al, log)

	importer := library.NewImporter(st, al, log)
	finder.WithHydrator(importer.Hydrate)
	malSync := library.NewMALSync(st, malClient, log)
	sync := library.NewSync(st, al, log).WithMAL(malSync).WithImporter(importer)
	watcher := library.NewWatcher(st, finder, sites.General(), log)
	scheduler := jobs.New(log)

	torrentEngine, err := newTorrentEngine(cfg, log)
	if err != nil {
		return err
	}
	defer torrentEngine.Stop()

	torrents := torrentEngine.Client()
	presence := library.NewPresence(st, log)
	applyPrefs := func() bool {
		prefs, err := st.Prefs(ctx, 0)
		if err != nil {
			return false
		}
		torrents.SetLimits(library.TorrentLimits(prefs))
		presence.Apply(prefs)
		return true
	}
	if applyPrefs() {
		if err := torrentEngine.Ensure(ctx); err != nil {
			log.Warn("torrent engine not running; it starts when something needs it", "err", err)
		}
	} else {
		log.Warn("settings unreadable at start; torrents wait until they can be read")
		go func() {
			for !applyPrefs() {
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
			}
			torrentEngine.Ensure(ctx)
		}()
	}

	mpv := player.New(cfg.Tool("mpv"), "", log)
	mpv.Warm()
	vlcBinary := player.ResolveVLC(cfg.VLCPath())
	if want := cfg.VLCPath(); want != "" && vlcBinary == "" {
		log.Warn("vlc_path does not exist", "path", want)
	}
	prefetcher := library.NewPrefetcher(st, finder, torrents, log)
	relations := library.NewRelations(st, al, log)
	ingester.Relations = relations
	finder.WithFranchise(relations.Ensure)

	playback := library.NewPlayback(st, finder, torrents, mpv, cfg.CacheDir, log).
		WithSync(sync).
		WithEnricher(enricher).
		WithPrefetcher(prefetcher).
		WithProber(transcode.NewProber(cfg.Tool("ffprobe"))).
		WithRelations(relations).
		WithPresence(presence).
		WithPlayer("vlc", player.NewVLC(vlcBinary, log))

	downloader := library.NewDownloader(st, prefetcher, func(ctx context.Context, animeID int) score.Preferences {
		prefs, err := st.Prefs(ctx, animeID)
		if err != nil {
			return score.DefaultPreferences()
		}
		return library.Preferences(prefs)
	}, log).WithRoom(cfg.CacheDir)
	if err := st.UnpinAll(ctx); err != nil {
		log.Warn("clear stale cache pins", "err", err)
	}
	downloader.Quiet(ctx)
	go downloader.Run(ctx)

	watcher.WithDownloader(downloader)

	if live, err := torrents.Live(ctx); err != nil {
		log.Warn("list torrents for reconcile", "err", err)
	} else if matched, orphaned, err := st.ReconcileTorrents(ctx, live); err != nil {
		log.Warn("reconcile torrents", "err", err)
	} else if matched+orphaned > 0 {
		log.Info("torrents reconciled", "matched", matched, "orphaned", orphaned)
	}
	cache := library.NewCache(st, torrents, cfg.CacheDir, log).WithClaimed(prefetcher.Claimed).
		WithProtected(filepath.Dir(cfg.ConfigPath()), cfg.DataDir(), cfg.BinDir)
	playback.WithCache(cache)
	prefetcher.WithRoom(cache.MakeRoom)
	scheduler.Add(jobs.Job{
		Name: "cache-sweep", Every: 2 * time.Minute,
		Run: func(ctx context.Context) error { _, err := cache.Sweep(ctx); return err },
	})

	scheduler.Add(jobs.Job{
		Name: "anilist-sync", Every: interval(st, "sync.poll_seconds", 15*time.Minute),
		Run: func(ctx context.Context) error { _, err := sync.Run(ctx); return err },
	})
	scheduler.Add(jobs.Job{
		Name: "release-watch", Every: interval(st, "notify.poll_seconds", 30*time.Minute), OnStart: true,
		Run: func(ctx context.Context) error { _, err := watcher.Poll(ctx); return err },
	})
	scheduler.Add(jobs.Job{
		Name: "filler-data", Every: 24 * time.Hour, OnStart: true,
		Run: func(ctx context.Context) error { _, err := enricher.Fillers(ctx, false); return err },
	})
	scheduler.Add(jobs.Job{
		Name: "seadex", Every: 24 * time.Hour, OnStart: true,
		Run: func(ctx context.Context) error { _, err := enricher.SeaDex(ctx, false); return err },
	})
	scheduler.Add(jobs.Job{
		Name: "group-tiers", Every: 24 * time.Hour, OnStart: true,
		Run: func(ctx context.Context) error { _, err := enricher.GroupTiers(ctx, false); return err },
	})
	scheduler.Add(jobs.Job{
		Name: "mal-sync", Every: interval(st, "sync.poll_seconds", 15*time.Minute),
		Run: func(ctx context.Context) error {
			if _, err := malSync.Run(ctx); err != nil {
				return err
			}
			_, err := malSync.Pull(ctx)
			return err
		},
	})

	var updater *update.Updater
	if exe != "" && update.Version != "dev" {
		updater = update.New(st, exe, filepath.Join(cfg.CacheDir, "update"), log).
			WithRestart(cancel)
		updater.Started(ctx)
		scheduler.Add(jobs.Job{
			Name: "update-check", Every: 6 * time.Hour, OnStart: true,
			Run: func(ctx context.Context) error { _, err := updater.Check(ctx); return err },
		})
	}

	streams := transcode.NewManager(
		cfg.Tool("ffmpeg"), cfg.Tool("ffprobe"),
		cfg.CacheDir, "libx264", log)
	if _, err := streams.Purge(); err != nil {
		log.Warn("purge transcode output", "err", err)
	}
	detectEncoder := func(ctx context.Context) {
		encoder := transcode.DetectEncoder(ctx, cfg.Tool("ffmpeg"), log)
		streams.SetEncoder(encoder)
		tuning := transcode.Tuning(ctx, cfg.Tool("ffmpeg"), encoder)
		streams.SetTuning(encoder, tuning)
		streams.SetToneMapper(transcode.ToneMapper(ctx, cfg.Tool("ffmpeg")))
		finder.WithHardwareTranscode(transcode.IsHardwareEncoder(encoder))
		log.Info("video encoder", "encoder", encoder, "tuning", strings.Join(tuning, " "))
	}
	go detectEncoder(ctx)

	depsManager := deps.New(cfg.BinDir, log)
	if removed := depsManager.RemoveObsolete(); len(removed) > 0 {
		log.Info("removed tools Yoru no longer uses", "files", removed)
	}
	depsManager.OnInstalled(func(name string) {
		if name != "ffmpeg" {
			return
		}
		detect, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		detectEncoder(detect)
	})

	streams.OnArriving(func(ctx context.Context, sessionID string) int64 {
		animeID, episode, ok := parseStreamID(sessionID)
		if !ok {
			return 0
		}
		hash, _, err := st.CachedFile(ctx, animeID, strconv.Itoa(episode))
		if err != nil || hash == "" {
			return 0
		}
		return cache.Downloaded(ctx, hash)
	})

	streams.OnSeek(func(sessionID string, fraction float64) {
		animeID, episode, ok := parseStreamID(sessionID)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		hash, index, err := st.CachedFile(ctx, animeID, strconv.Itoa(episode))
		if err != nil || hash == "" {
			return
		}
		live, err := torrents.Live(ctx)
		if err != nil {
			return
		}
		id, ok := live[strings.ToLower(hash)]
		if !ok {
			return
		}
		if err := torrents.PrioritiseAt(ctx, id, index, fraction); err != nil {
			log.Debug("prioritise at seek", "torrent", id, "err", err)
		}
	})

	var api *server.Server
	streams.OnIdle(func(sessionID string) {
		if api == nil {
			downloader.Release(sessionID)
			return
		}
		api.SessionIdle(sessionID)
	})

	window := &config.Window{
		Profile: cfg.ProfileDir(),
		Log:     log,
		Maximized: func() bool {
			mode, _ := st.Setting(context.Background(), "window.mode")
			return mode == "maximized"
		},
		Closed: func() {
			if mode, _ := st.Setting(context.Background(), "window.on_close"); mode == "tray" {
				log.Info("window closed; Yoru keeps running in the tray")
				return
			}
			log.Info("window closed; exiting")
			cancel()
		},
	}

	var showWindow func()
	if windowWanted() {
		showWindow = func() { window.Show(ctx, cfg.LocalURL()) }
	}

	api = server.New(server.Deps{
		Config:     cfg,
		Streams:    streams,
		Subtitles:  transcode.NewSubtitles(cfg.Tool("ffmpeg")),
		Thumbs:     transcode.NewThumbnails(cfg.Tool("ffmpeg"), log),
		Store:      st,
		AniList:    al,
		Importer:   importer,
		Ingester:   ingester,
		Playback:   playback,
		Watcher:    watcher,
		Enricher:   enricher,
		Relations:  relations,
		Cache:      cache,
		Jobs:       scheduler,
		MAL:        malClient,
		MALSync:    malSync,
		Scanner:    library.NewScanner(st, log).WithCacheDir(cfg.CacheDir),
		Sync:       sync,
		Prefetch:   prefetcher,
		Downloader: downloader,
		Updater:    updater,
		Engine:     torrents,
		Presence:   presence,
		Relaunch:   relaunchSelf(exe, cancel),
		Exit: func() {
			log.Info("exit chosen in the app")
			cancel()
		},
		ShowWindow: showWindow,
		Token:      token,
		Player:     mpv,
		Indexer:    sites.General(),
		Sites:      sites,
		Push:       pusher,
		Finder:     finder,
		Deps:       depsManager,
		Log:        log,
	})
	go streams.Reap(ctx)

	scheduler.Add(jobs.Job{
		Name: "corpus", Every: 24 * time.Hour, OnStart: true,
		Run: func(ctx context.Context) error {
			rep, err := ingester.Run(ctx, false)
			if rep.Seeded == 0 && rep.Fetched == 0 {
				return err
			}
			return errors.Join(err, api.RebuildIndex(ctx))
		},
	})
	importer.OnNewTitles = debounce(2*time.Minute, func() {
		if err := api.RebuildIndex(ctx); err != nil {
			log.Warn("rebuild match index", "err", err)
		}
	})
	scheduler.Add(jobs.Job{
		Name: "library-scan", Every: 30 * time.Minute,
		Run: api.ScanLibrary,
	})
	scheduler.Add(jobs.Job{
		Name: "airing-refresh", Every: 15 * time.Minute, OnStart: true,
		Run: func(ctx context.Context) error {
			_, err := importer.Refresh(anilist.Background(ctx), 150)
			return err
		},
	})
	scheduler.Start(ctx)

	go func() {
		if err := api.RebuildIndex(ctx); err != nil {
			log.Warn("match index unavailable", "err", err)
		}
	}()
	go api.WarmRankings(anilist.Background(ctx))
	go api.KeepAwake(ctx, power.New(log))
	go api.AutoUpdate(ctx)

	srv := &http.Server{
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	binder := &binder{srv: srv, log: log, claimed: claim}
	secure := certs.New(filepath.Join(cfg.DataDir(), "certs"), server.PairingIP, log)
	if cert, key := cfg.TLSFiles(); cert != "" {
		if err := secure.UseFiles(cert, key); err != nil {
			log.Error("tls_cert / tls_key not usable", "cert", cert, "err", err)
		}
	}
	binder.tls = secure.TLSConfig()
	api.SetCerts(secure)
	api.StartDuckDNS(ctx)
	rooms := tunnel.New(func() string { return cfg.Tool("cloudflared") }, log)
	defer rooms.Close()
	api.SetTunnel(rooms)
	api.OnRebind(binder.bind)

	drained := make(chan struct{})
	go func() {
		defer close(drained)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()

	addr := cfg.Addr
	if open, _ := st.Setting(ctx, "access.lan"); open == "true" {
		addr = server.LANAddr(cfg.Addr)
	}
	if err := binder.bind(addr); err != nil {
		return err
	}
	serving = true
	if exe != "" {
		update.Cleanup(exe)
	}
	if len(api.PairingURLs()) > 0 {
		go api.LogFirewall(ctx)
	}
	for _, link := range api.PairingURLs() {
		if u, err := url.Parse(link); err == nil {
			u.RawQuery = ""
			log.Info("reachable on this network; pair a device from Settings > Devices", "url", u.String())
		}
	}

	if windowWanted() {
		icon, err := tray.Start(tray.Options{
			Tip:  "Yoru",
			Open: func() { window.Show(ctx, cfg.LocalURL()) },
			Exit: func() {
				log.Info("exit chosen in the tray")
				cancel()
			},
			Ending: func() {
				log.Info("Windows is ending the session; stopping")
				cancel()
				select {
				case <-stopped:
				case <-time.After(4 * time.Second):
				}
			},
		})
		if err != nil {
			log.Warn("tray icon", "err", err)
		}
		defer icon.Close()
		if !startedInTray() || err != nil {
			go window.Open(ctx, cfg.LocalURL())
		}
	}

	<-ctx.Done()
	<-drained
	window.Close()
	return nil
}

type binder struct {
	srv *http.Server
	log *slog.Logger
	tls *tls.Config

	mu      sync.Mutex
	ln      net.Listener
	claimed net.Listener
}

func (b *binder) bind(addr string) error {
	ln := b.takeClaim(addr)
	if ln == nil {
		var err error
		if ln, err = net.Listen("tcp", addr); err != nil {
			b.mu.Lock()
			previous := b.ln
			b.mu.Unlock()
			if previous == nil {
				return err
			}
			previous.Close()
			if ln, err = net.Listen("tcp", addr); err != nil {
				return err
			}
		}
	}
	if b.tls != nil {
		ln = server.DualListener(ln, b.tls)
	}

	b.mu.Lock()
	previous := b.ln
	b.ln = ln
	b.mu.Unlock()

	if previous != nil {
		previous.Close()
	}

	b.log.Info("listening", "addr", "http://"+ln.Addr().String())
	go func() {
		if err := b.srv.Serve(ln); err != nil &&
			!errors.Is(err, net.ErrClosed) && !errors.Is(err, http.ErrServerClosed) {
			b.log.Error("serve", "addr", ln.Addr().String(), "err", err)
			time.Sleep(2 * time.Second)
			b.mu.Lock()
			current := b.ln == ln
			b.mu.Unlock()
			if current {
				if err := b.bind(addr); err != nil {
					b.log.Error("listen again", "addr", addr, "err", err)
				}
			}
		}
	}()
	return nil
}

func (b *binder) takeClaim(addr string) net.Listener {
	b.mu.Lock()
	c := b.claimed
	b.claimed = nil
	b.mu.Unlock()
	if c == nil {
		return nil
	}
	if c.Addr().String() == addr {
		return c
	}
	c.Close()
	return nil
}

func relaunchSelf(exe string, cancel context.CancelFunc) func() {
	if exe == "" {
		return nil
	}
	return func() {
		if err := update.RelaunchSelf(exe); err == nil {
			cancel()
		}
	}
}

func askToShow(base string) bool {
	config.AllowForeground()
	client := http.Client{Timeout: 3 * time.Second}
	res, err := client.Post(strings.TrimSuffix(base, "/")+"/api/app/show", "application/json", nil)
	if err != nil {
		return false
	}
	res.Body.Close()
	return res.StatusCode == http.StatusNoContent
}

func yoruAnswers(base string) bool {
	client := http.Client{Timeout: 2 * time.Second}
	res, err := client.Get(strings.TrimSuffix(base, "/") + "/api/health")
	if err != nil {
		return false
	}
	defer res.Body.Close()
	var body struct {
		Version string `json:"version"`
	}
	return res.StatusCode == http.StatusOK && json.NewDecoder(res.Body).Decode(&body) == nil && body.Version != ""
}

func warnSlowDisk(log *slog.Logger, dirs ...string) {
	seen := map[string]bool{}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		root := filepath.VolumeName(dir)
		if seen[root] {
			continue
		}
		seen[root] = true

		if d := proc.DriveOf(dir); d.Slow {
			log.Warn("Yoru is running from a slow disk; downloads and playback will be slow",
				"path", dir, "disk", d.Kind,
				"fix", "move the Yoru folder to an internal drive")
		}
	}
}
