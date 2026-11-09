package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"yoru/internal/config"
	"yoru/internal/deps"
	"yoru/internal/indexer"
	"yoru/internal/player"
	"yoru/internal/torrent"
	"yoru/internal/transcode"
)

type Component struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Purpose  string `json:"purpose"`
	Size     string `json:"size"`
	Required bool   `json:"required"`
	Role     string `json:"role,omitempty"`
	Present  bool   `json:"present"`
	Version  string `json:"version,omitempty"`
	Latest   string `json:"latest,omitempty"`
	Needs    string `json:"needs,omitempty"`
	Manual   string `json:"manual,omitempty"`
	System   bool   `json:"system,omitempty"`
	Problem  string `json:"problem,omitempty"`
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	versions := s.installedVersions()

	components := []Component{
		{
			Name: "engine", Label: "Torrent engine",
			Purpose:  "Built into Yoru: streams and downloads episodes. Nothing to install.",
			Required: true,
			Present:  true,
			Version:  "built in",
		},
		{
			Name: "ffmpeg", Label: "Transcoder",
			Purpose: "ffmpeg and ffprobe. Prepares every episode for the built-in player: in a " +
				"browser, on a phone or on a TV. Not needed if you only play in mpv or VLC.",
			Size:    "420 MB",
			Role:    "Needed for the built-in player",
			Present: s.hasBinary("ffmpeg") && s.hasBinary("ffprobe"),
			Problem: s.halfTranscoder(),
			Version: versions["ffmpeg"],
		},
		{
			Name: "mpv", Label: "Desktop player",
			Purpose: "Optional external player. Better performance than a browser, " +
				"but desktop only.",
			Size:    "115 MB",
			Present: s.hasBinary("mpv"),
			Version: versions["mpv"],
		},
		{
			Name: "anime4k", Label: "Anime4K shaders",
			Purpose: "Upscaling shaders for mpv. The browser player uses its own " +
				"WebGPU build and needs no download.",
			Size:    "1 MB",
			Present: s.hasShaders(),
			Version: versions["anime4k"],
			Needs:   "mpv",
		},
		{
			Name: "flyhigh", Label: "Smooth motion",
			Purpose: "FlyHigh frame generation for the built-in player: frames between the episode's own, made " +
				"on the watching device's graphics card. Turned on per device from the player.",
			Size:    "2 MB",
			Present: s.hasFlyHigh(),
			Version: versions["flyhigh"],
		},
		{
			Name: "cloudflared", Label: "Watch together",
			Purpose: "Cloudflare's tunnel, so friends anywhere can join your watch-together room. No account or " +
				"router setup. Runs only while a room is open.",
			Size:    "52 MB",
			Present: s.hasBinary("cloudflared"),
			Version: versions["cloudflared"],
		},
	}

	for i := range components {
		if components[i].Name == "ffmpeg" && components[i].Present {
			s.describeSystemFFmpeg(r.Context(), &components[i])
		}
	}
	for i := range components {
		if !components[i].Present {
			components[i].Manual = deps.ManualCommand(components[i].Name)
		}
	}
	if s.deps != nil {
		for i := range components {
			if !components[i].Present || components[i].System || !deps.Known(components[i].Name) {
				continue
			}
			components[i].Latest = s.deps.LatestKnown(components[i].Name)
			if components[i].Latest == "" {
				go s.refreshLatest(components[i].Name)
			}
		}
	}

	cache, _ := s.store.SettingInt(r.Context(), "cache.budget_bytes")

	roots, _ := s.scannerRoots(r)
	if roots == nil {
		roots = []string{}
	}

	ready := true
	for _, c := range components {
		if c.Required && !c.Present {
			ready = false
		}
	}

	usable, bad := 0, []string{}
	sites, _, _ := LoadSites(r.Context(), s.store, s.cfg)
	for _, site := range sites {
		if _, err := indexer.Build(site.Type, site.URL, site.Adult); err != nil {
			bad = append(bad, err.Error())
		} else {
			usable++
		}
	}

	body := map[string]any{
		"components":     components,
		"ready":          ready,
		"indexers":       usable,
		"badIndexers":    bad,
		"defaultDataDir": config.DefaultDataDir(),
		"configPath":     s.cfg.ConfigPath(),
		"dataDir":        s.cfg.DataDir(),
		"strayConfig":    s.cfg.StrayConfig(),
		"vlc":            player.ResolveVLC(s.cfg.VLCPath()),
		"temporary":      s.cfg.Temporary(),
		"binDir":         s.cfg.BinDir,
		"cacheDir":       s.cfg.CacheDir,
		"cacheBudget":    cache,
		"libraryPaths":   roots,
		"progress":       []any{},
	}
	if s.deps != nil {
		body["progress"] = s.deps.Status()
	}
	if s.streams != nil {
		body["encoder"], body["toneMapper"] = s.streams.Converter()
	}
	send(w, http.StatusOK, body)
}

func (s *Server) setDataDir(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path        string `json:"path"`
		UseExisting bool   `json:"useExisting"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&body); err != nil {
		send(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	path := strings.TrimSpace(body.Path)
	if strings.Contains(path, "..") {
		send(w, http.StatusBadRequest, map[string]any{"error": "Choose the folder by its full path, without \"..\"."})
		return
	}
	dir := s.cfg.ResolveDataDir(path)
	if dir == s.cfg.DataDir() {
		send(w, http.StatusOK, map[string]any{"dataDir": dir, "copied": false, "restart": false})
		return
	}
	existing := ""
	if p := filepath.Join(dir, config.Name+".db"); fileExists(p) {
		existing = p
	}
	if info, err := os.Stat(existing); existing != "" && err == nil && !body.UseExisting {
		send(w, http.StatusConflict, map[string]any{
			"error":    "that folder already holds a Yoru database",
			"existing": true,
			"modified": info.ModTime().Unix(),
		})
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		send(w, http.StatusBadRequest, map[string]any{"error": "cannot create folder: " + err.Error()})
		return
	}
	probe, err := os.CreateTemp(dir, ".yoru-*")
	if err != nil {
		send(w, http.StatusBadRequest, map[string]any{"error": "folder is not writable"})
		return
	}
	probe.Close()
	os.Remove(probe.Name())

	target := filepath.Join(dir, config.Name+".db")
	copied := false
	if dir != s.cfg.DataDir() && existing == "" && fileExists(s.cfg.DatabasePath()) {
		if err := s.store.SnapshotTo(r.Context(), target); err != nil {
			os.Remove(target)
			s.fail(w, "copy database", err)
			return
		}
		copied = true
	}
	if err := s.cfg.SetDataDir(path); err != nil {
		s.fail(w, "write config", err)
		return
	}
	if s.relaunch != nil {
		go func() {
			time.Sleep(time.Second)
			s.relaunch()
		}()
	}
	send(w, http.StatusOK, map[string]any{"dataDir": dir, "copied": copied, "restart": s.relaunch != nil})
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

const latestRetry = 10 * time.Minute

func (s *Server) refreshLatest(name string) {
	s.latestMu.Lock()
	if s.latestJobs == nil {
		s.latestJobs = map[string]time.Time{}
	}
	if at, running := s.latestJobs[name]; running && time.Since(at) < latestRetry {
		s.latestMu.Unlock()
		return
	}
	s.latestJobs[name] = time.Now()
	s.latestMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := s.deps.Latest(ctx, name); err != nil {
		s.log.Debug("latest version", "component", name, "err", err)
	}
}

func (s *Server) installComponent(w http.ResponseWriter, r *http.Request) {
	if s.deps == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{
			"error": "installer unavailable",
		})
		return
	}

	name := r.PathValue("name")
	if !deps.Known(name) {
		send(w, http.StatusNotFound, map[string]any{"error": "unknown component"})
		return
	}
	if err := s.deps.Install(name); err != nil {
		s.fail(w, "install component", err)
		return
	}
	send(w, http.StatusAccepted, map[string]any{"installing": name})
}

type systemTool struct {
	modTime   time.Time
	version   string
	canEncode bool
}

func (s *Server) describeSystemFFmpeg(ctx context.Context, c *Component) {
	path := s.cfg.Tool("ffmpeg")
	if torrent.Within(s.cfg.BinDir, path) {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	s.systemMu.Lock()
	t, ok := s.systemFF[path]
	s.systemMu.Unlock()
	if !ok || !t.modTime.Equal(info.ModTime()) {
		probe, cancel := context.WithTimeout(ctx, time.Minute)
		t = systemTool{
			modTime:   info.ModTime(),
			version:   transcode.Version(probe, path),
			canEncode: transcode.CanEncode(probe, path),
		}
		cancel()
		s.systemMu.Lock()
		if s.systemFF == nil {
			s.systemFF = map[string]systemTool{}
		}
		s.systemFF[path] = t
		s.systemMu.Unlock()
	}
	c.System = true
	c.Version = t.version
	if !t.canEncode {
		c.Present = false
		c.Problem = "The ffmpeg on this system cannot encode H.264 (no libx264, no working hardware encoder)."
	}
}

func (s *Server) hasBinary(name string) bool {
	info, err := os.Stat(s.cfg.Path(config.ExeName(name)))
	if err == nil && !info.IsDir() && info.Size() > 0 {
		return true
	}
	_, err = exec.LookPath(config.ExeName(name))
	return err == nil
}

func (s *Server) halfTranscoder() string {
	ffmpeg, ffprobe := s.hasBinary("ffmpeg"), s.hasBinary("ffprobe")
	switch {
	case ffmpeg && !ffprobe:
		return "ffmpeg is on this system but ffprobe is not; Yoru needs both. Install fetches a matching pair."
	case ffprobe && !ffmpeg:
		return "ffprobe is on this system but ffmpeg is not; Yoru needs both. Install fetches a matching pair."
	}
	return ""
}

func (s *Server) hasShaders() bool {
	matches, err := filepath.Glob(filepath.Join(s.cfg.BinDir, "shaders", "*.glsl"))
	return err == nil && len(matches) > 10
}

func (s *Server) scannerRoots(r *http.Request) ([]string, error) {
	if s.scanner == nil {
		return nil, nil
	}
	return s.scanner.Roots(r.Context())
}

func (s *Server) installedVersions() map[string]string {
	out := map[string]string{}

	raw, err := os.ReadFile(filepath.Join(s.cfg.BinDir, "versions.json"))
	if err != nil {
		return out
	}
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]string{}
	}
	return out
}
