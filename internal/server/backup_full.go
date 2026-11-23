package server

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"yoru/internal/config"
	"yoru/internal/db"
)

const backupLimit = 8 << 30

func (s *Server) fullBackup(w http.ResponseWriter, r *http.Request) {
	tmp, err := os.CreateTemp(s.cfg.DataDir(), ".backup-*.db")
	if err != nil {
		s.fail(w, "full backup", err)
		return
	}
	path := tmp.Name()
	tmp.Close()
	os.Remove(path)
	defer os.Remove(path)
	if err := s.store.SnapshotTo(r.Context(), path); err != nil {
		s.fail(w, "full backup", err)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		s.fail(w, "full backup", err)
		return
	}
	defer f.Close()
	name := config.Name + "-backup-" + time.Now().Format("2006-01-02") + ".db"
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, name, time.Now(), f)
}

func (s *Server) fullRestore(w http.ResponseWriter, r *http.Request) {
	target := db.RestorePath(s.cfg.DatabasePath())
	tmp, err := os.CreateTemp(filepath.Dir(target), ".restore-*.db")
	if err != nil {
		s.fail(w, "restore", err)
		return
	}
	path := tmp.Name()
	_, err = io.Copy(tmp, http.MaxBytesReader(w, r.Body, backupLimit))
	tmp.Close()
	defer os.Remove(path)
	if err != nil {
		send(w, http.StatusBadRequest, map[string]any{"error": "the file didn't arrive whole"})
		return
	}
	if err := db.CheckBackup(path); err != nil {
		msg := err.Error()
		if errors.Is(err, db.ErrNotBackup) {
			msg = "That file isn't a Yoru backup."
		}
		send(w, http.StatusBadRequest, map[string]any{"error": msg})
		return
	}
	os.Remove(target)
	if err := os.Rename(path, target); err != nil {
		s.fail(w, "restore", err)
		return
	}
	if s.relaunch != nil {
		go func() {
			time.Sleep(time.Second)
			s.relaunch()
		}()
	}
	send(w, http.StatusOK, map[string]any{"restart": s.relaunch != nil})
}
