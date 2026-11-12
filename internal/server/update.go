package server

import (
	"net/http"

	"yoru/internal/update"
)

func (s *Server) updateStatus(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		send(w, http.StatusOK, map[string]any{"current": update.Version, "available": false, "stage": update.StageIdle, "bytes": 0, "total": 0, "fixed": true})
		return
	}
	send(w, http.StatusOK, s.updateView(r))
}

type updateView struct {
	update.Status
	Auto bool     `json:"auto"`
	Busy []string `json:"busy,omitempty"`
}

func (s *Server) updateView(r *http.Request) updateView {
	out := updateView{Status: s.updater.Status(), Auto: s.autoUpdates(r.Context())}
	if out.Stage == update.StageReady {
		out.Busy = s.busyWith(r.Context())
	}
	return out
}

func (s *Server) updateRestart(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "updater unavailable"})
		return
	}
	if err := s.updater.Install(); err != nil {
		send(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	send(w, http.StatusAccepted, s.updateView(r))
}

func (s *Server) updatedSince(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"current": update.Version}
	if s.updater != nil {
		out["updatedFrom"] = s.updater.Status().UpdatedFrom
	}
	send(w, http.StatusOK, out)
}

func (s *Server) updateCheck(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "updater unavailable"})
		return
	}
	if _, err := s.updater.Check(r.Context()); err != nil {
		s.log.Warn("update check", "err", err)
	}
	send(w, http.StatusOK, s.updateView(r))
}

func (s *Server) updateDownload(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "updater unavailable"})
		return
	}
	if err := s.updater.Prepare(); err != nil {
		send(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	send(w, http.StatusAccepted, s.updateView(r))
}
