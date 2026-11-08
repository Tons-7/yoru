package server

import (
	"net/http"
	"path/filepath"
	"slices"

	"yoru/internal/deps"
)

func (s *Server) hasFlyHigh() bool {
	for _, f := range deps.FlyHighFiles {
		if !fileExists(filepath.Join(s.cfg.BinDir, deps.FlyHighDir, f)) {
			return false
		}
	}
	return true
}

func (s *Server) refreshFlyHigh() {
	if s.deps == nil || s.cfg.BinDir == "" {
		return
	}
	if v := s.installedVersions()["flyhigh"]; v != "" && v != deps.FlyHighVersion {
		if err := s.deps.Install("flyhigh"); err != nil {
			s.log.Warn("update smooth motion files", "from", v, "to", deps.FlyHighVersion, "err", err)
		}
	}
}

func (s *Server) smoothProgress() *deps.Progress {
	if s.deps == nil {
		return nil
	}
	for _, p := range s.deps.Status() {
		if p.Component == "flyhigh" {
			return &p
		}
	}
	return nil
}

func (s *Server) smoothReady() bool {
	if p := s.smoothProgress(); p != nil && p.Stage != deps.StageDone && p.Stage != deps.StageFailed {
		return false
	}
	return s.hasFlyHigh() && s.installedVersions()["flyhigh"] == deps.FlyHighVersion
}

func (s *Server) smoothStatus(w http.ResponseWriter, r *http.Request) {
	body := map[string]any{
		"ready":      s.smoothReady(),
		"version":    s.installedVersions()["flyhigh"],
		"wanted":     deps.FlyHighVersion,
		"canInstall": fromHost(r) && s.deps != nil,
	}
	if p := s.smoothProgress(); p != nil {
		body["progress"] = p
	}
	send(w, http.StatusOK, body)
}

var smoothTypes = map[string]string{
	".js":   "text/javascript; charset=utf-8",
	".wasm": "application/wasm",
	".json": "application/json",
	".bin":  "application/octet-stream",
}

func (s *Server) smoothFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if !slices.Contains(deps.FlyHighFiles, name) {
		send(w, http.StatusNotFound, map[string]any{"error": "not a smooth motion file"})
		return
	}
	if !s.smoothReady() {
		send(w, http.StatusNotFound, map[string]any{"error": "smooth motion is not installed on the PC"})
		return
	}
	path := filepath.Join(s.cfg.BinDir, deps.FlyHighDir, name)
	w.Header().Set("Content-Type", smoothTypes[filepath.Ext(name)])
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, path)
}
