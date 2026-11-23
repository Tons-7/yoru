package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"yoru/internal/library"
	"yoru/internal/match"
	"yoru/internal/store"
	"yoru/internal/torrent"
)

func (s *Server) localStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.store.LocalStats(r.Context())
	if err != nil {
		s.fail(w, "local stats", err)
		return
	}

	roots, err := s.scannerRoots(r)
	if err != nil {
		s.log.Warn("library roots", "err", err)
	}
	send(w, http.StatusOK, map[string]any{
		"stats":    stats,
		"roots":    roots,
		"lastScan": s.lastScan(),
		"scanning": s.scanning.Load(),
	})
}

func (s *Server) localFiles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	animeID, _ := strconv.Atoi(q.Get("anime"))

	page, err := s.store.LocalFilesView(r.Context(), animeID,
		q.Get("unmatched") == "true", q.Get("missing") == "true", store.ParsePaging(q, 60, 200))
	if err != nil {
		s.fail(w, "local files", err)
		return
	}
	if !fromHost(r) {
		for i := range page.Items {
			page.Items[i].Path = filepath.Base(page.Items[i].Path)
		}
	}
	send(w, http.StatusOK, page)
}

func (s *Server) localScan(w http.ResponseWriter, r *http.Request) {
	roots, err := s.scanner.Roots(r.Context())
	if err != nil {
		s.fail(w, "library roots", err)
		return
	}
	if len(roots) == 0 {
		send(w, http.StatusPreconditionFailed, map[string]any{
			"error": "no directories configured; set library.paths first",
		})
		return
	}
	if s.index.get() == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "Still building the title index; try again in a moment."})
		return
	}
	status := "started"
	if s.scanning.Load() {
		status = "queued"
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
		defer cancel()
		if err := s.scan(ctx, s.index.get()); err != nil {
			s.log.Error("library scan", "err", err)
		}
	}()

	send(w, http.StatusAccepted, map[string]any{"status": status, "roots": roots})
}

func (s *Server) ScanLibrary(ctx context.Context) error {
	ix := s.index.get()
	if ix == nil {
		return nil
	}
	return s.scan(ctx, ix)
}

func (s *Server) scan(ctx context.Context, ix *match.Index) error {
	for !s.scanning.CompareAndSwap(false, true) {
		s.rescan.Store(true)
		if s.scanning.Load() {
			return nil
		}
	}
	for {
		s.rescan.Store(false)
		_, err := s.scanner.Scan(ctx, ix)
		s.scanning.Store(false)
		if err != nil || !s.rescan.Load() || !s.scanning.CompareAndSwap(false, true) {
			return err
		}
	}
}

func (s *Server) lastScan() *library.ScanReport {
	if s.scanner == nil {
		return nil
	}
	return s.scanner.LastReport()
}

func (s *Server) setLibraryPaths(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		send(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}

	var current []string
	if s.scanner != nil {
		current, _ = s.scanner.Roots(r.Context())
	}
	clean := make([]string, 0, len(body.Paths))
	problems := map[string]string{}
	for _, p := range body.Paths {
		if p == "" {
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			problems[p] = err.Error()
			continue
		}
		if slices.ContainsFunc(clean, func(c string) bool { return sameFolder(c, abs) }) {
			continue
		}
		if slices.Contains(current, abs) {
			clean = append(clean, abs)
			continue
		}
		info, err := os.Stat(abs)
		switch {
		case err != nil:
			problems[p] = "not readable"
		case !info.IsDir():
			problems[p] = "not a directory"
		case s.cfg.CacheDir != "" && (torrent.Within(s.cfg.CacheDir, abs) || torrent.Within(abs, s.cfg.CacheDir)):
			problems[p] = "holds Yoru's own downloads; a library folder must be elsewhere"
		default:
			clean = append(clean, abs)
		}
	}
	if len(problems) > 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "unusable paths", "paths": problems})
		return
	}

	encoded, err := json.Marshal(clean)
	if err != nil {
		s.fail(w, "library paths", err)
		return
	}
	if err := s.store.SetSetting(r.Context(), "library.paths", string(encoded)); err != nil {
		s.fail(w, "library paths", err)
		return
	}
	roots := make([]string, 0, len(clean))
	for _, root := range clean {
		if real, err := filepath.EvalSymlinks(root); err == nil {
			root = real
		}
		roots = append(roots, root)
	}
	if _, err := s.store.SweepOutside(r.Context(), roots); err != nil {
		s.log.Warn("retire files outside the library", "err", err)
	}
	if len(clean) > 0 {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
			defer cancel()
			if err := s.ScanLibrary(ctx); err != nil {
				s.log.Warn("library scan", "err", err)
			}
		}()
	}
	send(w, http.StatusOK, map[string]any{"paths": clean})
}

func (s *Server) forgetMissing(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.ForgetMissing(r.Context())
	if err != nil {
		s.fail(w, "forget missing", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"forgotten": n})
}

func (s *Server) assignLocalFile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID      int `json:"id"`
		AnimeID int `json:"animeId"`
		Episode int `json:"episode"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		send(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	if body.ID <= 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "id is required"})
		return
	}

	if body.AnimeID != 0 {
		if err := s.store.EnsureAnime(r.Context(), body.AnimeID); err != nil {
			s.fail(w, "ensure anime", err)
			return
		}
	}
	if err := s.store.AssignLocalFile(r.Context(), body.ID, body.AnimeID, body.Episode); err != nil {
		s.fail(w, "assign local file", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"id": body.ID, "animeId": body.AnimeID, "episode": body.Episode})
}

func (s *Server) localStream(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	if id <= 0 {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}

	f, err := s.store.LocalFile(r.Context(), id)
	if err != nil {
		s.log.Error("local file", "err", err)
		http.Error(w, "lookup failed", http.StatusBadGateway)
		return
	}
	if f.ID == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	file, err := os.Open(f.Path)
	if err != nil {
		http.Error(w, "file unavailable", http.StatusNotFound)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		http.Error(w, "file unavailable", http.StatusNotFound)
		return
	}

	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeContent(w, r, filepath.Base(f.Path), info.ModTime(), file)
}

func sameFolder(a, b string) bool {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
