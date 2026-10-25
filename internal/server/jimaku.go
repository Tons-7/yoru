package server

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"yoru/internal/jimaku"
	"yoru/internal/proc"
)

// Japanese subtitles from jimaku.cc, with the viewer's key: a secret setting (.token), never sent back.

const jimakuKey = "jimaku.token"

// An episode's files are asked for again after this; a menu opened twice shouldn't ask twice.
const jimakuFresh = 10 * time.Minute

type jimakuListing struct {
	files []jimaku.File
	at    time.Time
}

// jimakuState says whether a key is set, so the player offers Jimaku only then.
func (s *Server) jimakuState(w http.ResponseWriter, r *http.Request) {
	key, _ := s.store.Setting(r.Context(), jimakuKey)
	send(w, http.StatusOK, map[string]any{"set": key != ""})
}

// setJimakuKey keeps a key once Jimaku takes it; an empty one forgets it.
func (s *Server) setJimakuKey(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key string `json:"key"`
	}
	if !decode(w, r, &body) {
		return
	}
	key := strings.TrimSpace(body.Key)
	if key != "" {
		if err := s.jimaku.Check(r.Context(), key); err != nil {
			send(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
	}
	if err := s.store.SetSetting(r.Context(), jimakuKey, key); err != nil {
		s.fail(w, "jimaku key", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"set": key != ""})
}

// jimakuListFor is the episode's files, from the last listing while fresh.
func (s *Server) jimakuListFor(ctx context.Context, animeID, episode int) ([]jimaku.File, error) {
	key, _ := s.store.Setting(ctx, jimakuKey)
	if key == "" {
		return nil, errors.New("add a Jimaku API key under Settings, Language & display first")
	}
	// Negative ids are MAL-only shows, which Jimaku can't look up.
	if animeID <= 0 {
		return []jimaku.File{}, nil
	}
	id := fmt.Sprintf("%d-%d", animeID, episode)
	s.jimakuMu.Lock()
	cached, ok := s.jimakuFiles[id]
	s.jimakuMu.Unlock()
	if ok && time.Since(cached.at) < jimakuFresh {
		return cached.files, nil
	}
	files, err := s.jimaku.Files(ctx, key, animeID, episode)
	if err != nil {
		return nil, err
	}
	s.jimakuMu.Lock()
	maps.DeleteFunc(s.jimakuFiles, func(_ string, l jimakuListing) bool { return time.Since(l.at) >= jimakuFresh })
	s.jimakuFiles[id] = jimakuListing{files: files, at: time.Now()}
	s.jimakuMu.Unlock()
	return files, nil
}

// buildOnce makes path when it isn't there yet, one ask at a time: a second waits, then finds it made.
func (s *Server) buildOnce(path string, make func(part string) error) error {
	var mu *sync.Mutex
	for {
		held, _ := s.building.LoadOrStore(path, &sync.Mutex{})
		mu = held.(*sync.Mutex)
		mu.Lock()
		// SessionGone dropped it before the lock: a later caller would build beside this one under a new lock.
		if now, ok := s.building.Load(path); ok && now == held {
			break
		}
		mu.Unlock()
	}
	defer mu.Unlock()
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	part := path + ".part" + filepath.Ext(path)
	if err := make(part); err != nil {
		os.Remove(part)
		return err
	}
	return os.Rename(part, path)
}

// streamJimaku lists the playing episode's Japanese subtitle files, each a track the player can load.
func (s *Server) streamJimaku(w http.ResponseWriter, r *http.Request) {
	session, ok := s.stream(w, r)
	if !ok {
		return
	}
	animeID, episode, ok := parseSessionID(session.ID)
	if !ok {
		send(w, http.StatusOK, map[string]any{"files": []any{}})
		return
	}
	files, err := s.jimakuListFor(r.Context(), animeID, episode)
	if err != nil {
		send(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	type entry struct {
		Index int    `json:"index"`
		Name  string `json:"name"`
		URL   string `json:"url"`
	}
	out := make([]entry, len(files))
	for i, f := range files {
		// Numbered by the file too, so a picked track stays the same file when a fresh listing reorders; 48 bits,
		// as two files sharing a number would pick each other.
		n, _ := strconv.ParseInt(jimakuName(f)[:12], 16, 64)
		out[i] = entry{Index: int(n), Name: f.Name, URL: fmt.Sprintf("/api/stream/%s/jimaku/%s", session.ID, jimakuName(f))}
	}
	send(w, http.StatusOK, map[string]any{"files": out})
}

// jimakuName names a file by its own address, not its place in a listing that may come back in another order.
func jimakuName(f jimaku.File) string {
	sum := sha1.Sum([]byte(f.URL))
	return hex.EncodeToString(sum[:8])
}

// streamJimakuFile serves one listed file as ASS, the renderer's own format, converted once into the session.
func (s *Server) streamJimakuFile(w http.ResponseWriter, r *http.Request) {
	session, ok := s.stream(w, r)
	if !ok {
		return
	}
	animeID, episode, idOK := parseSessionID(session.ID)
	name := r.PathValue("n")
	if _, err := hex.DecodeString(name); !idOK || err != nil || len(name) != 16 {
		http.NotFound(w, r)
		return
	}
	// Converted already: served as it is, with no listing to ask Jimaku for (busy, or the key gone since).
	out := filepath.Join(session.Dir(), "jimaku-"+name+".ass")
	if _, err := os.Stat(out); err == nil {
		s.serveJimakuTrack(w, out)
		return
	}
	files, err := s.jimakuListFor(r.Context(), animeID, episode)
	if err != nil {
		send(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	at := slices.IndexFunc(files, func(f jimaku.File) bool { return jimakuName(f) == name })
	if at < 0 {
		http.NotFound(w, r)
		return
	}
	file := files[at]
	var fetchErr error
	err = s.buildOnce(out, func(part string) error {
		key, _ := s.store.Setting(r.Context(), jimakuKey)
		raw, err := s.jimaku.Fetch(r.Context(), key, file.URL)
		if err != nil {
			fetchErr = err
			return err
		}
		// ffmpeg reads the format by the name; never out's own name, or a failed conversion would pass as done.
		in := part + ".source" + strings.ToLower(path.Ext(file.Name))
		defer os.Remove(in)
		if err := os.WriteFile(in, raw, 0o644); err != nil {
			return err
		}
		cmd := proc.Hide(exec.CommandContext(r.Context(), s.cfg.Tool("ffmpeg"), "-hide_banner", "-loglevel", "error", "-y",
			"-i", in, "-c:s", "ass", part))
		if msg, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%v: %s", err, bytes.TrimSpace(msg))
		}
		return nil
	})
	if fetchErr != nil {
		send(w, http.StatusBadGateway, map[string]any{"error": fetchErr.Error()})
		return
	}
	if err != nil {
		s.fail(w, "jimaku file", err)
		return
	}
	s.serveJimakuTrack(w, out)
}

func (s *Server) serveJimakuTrack(w http.ResponseWriter, path string) {
	track, err := os.ReadFile(path)
	if err != nil {
		s.fail(w, "jimaku file", err)
		return
	}
	w.Header().Set("X-Yoru-Complete", "true")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(track)
}
