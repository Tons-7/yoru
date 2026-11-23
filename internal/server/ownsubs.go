package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

type ownSubtitle struct {
	Name string `json:"name"`
	ASS  string `json:"ass"`
}

func (s *Server) ownSubtitlePath(r *http.Request) (string, bool) {
	anime, err1 := strconv.Atoi(r.PathValue("anime"))
	episode, err2 := strconv.Atoi(r.PathValue("episode"))
	if err1 != nil || err2 != nil || anime == 0 || episode <= 0 {
		return "", false
	}
	return filepath.Join(s.cfg.DataDir(), "subtitles", fmt.Sprintf("%d-%d.json", anime, episode)), true
}

func (s *Server) getOwnSubtitle(w http.ResponseWriter, r *http.Request) {
	path, ok := s.ownSubtitlePath(r)
	if !ok {
		send(w, http.StatusBadRequest, map[string]any{"error": "bad episode"})
		return
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		s.fail(w, "read kept subtitle", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(b)
}

func (s *Server) putOwnSubtitle(w http.ResponseWriter, r *http.Request) {
	path, ok := s.ownSubtitlePath(r)
	if !ok {
		send(w, http.StatusBadRequest, map[string]any{"error": "bad episode"})
		return
	}
	var body ownSubtitle
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&body); err != nil || body.ASS == "" {
		send(w, http.StatusBadRequest, map[string]any{"error": "invalid subtitle"})
		return
	}
	if len(body.Name) > 200 {
		body.Name = body.Name[:200]
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		s.fail(w, "keep subtitle", err)
		return
	}
	b, err := json.Marshal(body)
	if err != nil {
		s.fail(w, "keep subtitle", err)
		return
	}
	if err := writeFileAtomic(path, b); err != nil {
		s.fail(w, "keep subtitle", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteOwnSubtitle(w http.ResponseWriter, r *http.Request) {
	path, ok := s.ownSubtitlePath(r)
	if !ok {
		send(w, http.StatusBadRequest, map[string]any{"error": "bad episode"})
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		s.fail(w, "forget subtitle", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeFileAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
