package server

import (
	"net/http"

	"yoru/internal/update"
)

func (s *Server) updateStatus(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "updater unavailable"})
		return
	}
	send(w, http.StatusOK, s.updater.Status())
}

// updatedSince: the running version and the one it replaced, for Settings' New tab; any build answers.
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
	st, err := s.updater.Check(r.Context())
	if err != nil {
		s.log.Warn("update check", "err", err)
	}
	send(w, http.StatusOK, st)
}

// updateApply starts the download; the page polls updateStatus and reloads on the new version.
func (s *Server) updateApply(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "updater unavailable"})
		return
	}
	if err := s.updater.Apply(); err != nil {
		send(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	send(w, http.StatusAccepted, s.updater.Status())
}
