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
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/japanese"

	"yoru/internal/jimaku"
	"yoru/internal/proc"
)

const jimakuKey = "jimaku.token"

const jimakuFresh = 10 * time.Minute

type jimakuListing struct {
	files []jimaku.File
	at    time.Time
}

func (s *Server) jimakuState(w http.ResponseWriter, r *http.Request) {
	key, _ := s.store.Setting(r.Context(), jimakuKey)
	send(w, http.StatusOK, map[string]any{"set": key != ""})
}

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

func (s *Server) jimakuListFor(ctx context.Context, animeID, episode int) ([]jimaku.File, error) {
	key, _ := s.store.Setting(ctx, jimakuKey)
	if key == "" {
		return nil, errors.New("add a Jimaku API key under Settings, Language & display first")
	}
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

func (s *Server) buildOnce(path string, make func(part string) error) error {
	var mu *sync.Mutex
	for {
		held, _ := s.building.LoadOrStore(path, &sync.Mutex{})
		mu = held.(*sync.Mutex)
		mu.Lock()
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
		Index int64  `json:"index"`
		Name  string `json:"name"`
		URL   string `json:"url"`
	}
	out := make([]entry, len(files))
	for i, f := range files {
		n, _ := strconv.ParseInt(jimakuName(f)[:12], 16, 64)
		out[i] = entry{Index: n, Name: f.Name, URL: fmt.Sprintf("/api/stream/%s/jimaku/%s", session.ID, jimakuName(f))}
	}
	send(w, http.StatusOK, map[string]any{"files": out})
}

func jimakuName(f jimaku.File) string {
	sum := sha1.Sum([]byte(f.URL))
	return hex.EncodeToString(sum[:8])
}

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
		in := part + ".source" + strings.ToLower(path.Ext(file.Name))
		defer os.Remove(in)
		if err := os.WriteFile(in, asUTF8(raw), 0o644); err != nil {
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

func asUTF8(raw []byte) []byte {
	if bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}) || bytes.HasPrefix(raw, []byte{0xFF, 0xFE}) ||
		bytes.HasPrefix(raw, []byte{0xFE, 0xFF}) || utf8.Valid(raw) {
		return raw
	}
	for _, enc := range []encoding.Encoding{japanese.ShiftJIS, japanese.EUCJP} {
		if out, err := enc.NewDecoder().Bytes(raw); err == nil && utf8.Valid(out) && !bytes.ContainsRune(out, utf8.RuneError) {
			return out
		}
	}
	return raw
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
