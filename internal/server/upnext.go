package server

import (
	"net/http"
	"strconv"
)

// upNext is the Up next list, top to bottom.
func (s *Server) upNext(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.UpNext(r.Context())
	if err != nil {
		s.fail(w, "up next", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) addUpNext(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AnimeID int `json:"animeId"`
		Episode int `json:"episode"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.AnimeID == 0 || body.Episode <= 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "animeId and episode are required"})
		return
	}
	added, err := s.store.AddUpNext(r.Context(), body.AnimeID, body.Episode)
	if err != nil {
		s.fail(w, "add to up next", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"added": added})
}

// removeUpNext takes one episode off, or without ?episode the whole show.
func (s *Server) removeUpNext(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("anime"))
	episode, _ := strconv.Atoi(r.URL.Query().Get("episode"))
	if id == 0 || episode < 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "a show is required"})
		return
	}
	if err := s.store.RemoveUpNext(r.Context(), id, episode); err != nil {
		s.fail(w, "remove from up next", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"removed": true})
}

func (s *Server) clearUpNext(w http.ResponseWriter, r *http.Request) {
	if err := s.store.ClearUpNext(r.Context()); err != nil {
		s.fail(w, "clear up next", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"cleared": true})
}

// moveUpNext puts a show at a place among the list's shows, 0 being the top.
func (s *Server) moveUpNext(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AnimeID int `json:"animeId"`
		To      int `json:"to"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.AnimeID == 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "animeId is required"})
		return
	}
	if err := s.store.MoveUpNext(r.Context(), body.AnimeID, body.To); err != nil {
		s.fail(w, "move in up next", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"moved": true})
}

// fillUpNext is "Add next episodes": one per show being watched, and how many that was.
func (s *Server) fillUpNext(w http.ResponseWriter, r *http.Request) {
	added, err := s.store.FillUpNext(r.Context())
	if err != nil {
		s.fail(w, "add next episodes", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"added": added})
}

// upNextAfter is what the player's card offers once an episode ends: the list's next two, when it has any.
func (s *Server) upNextAfter(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.URL.Query().Get("id"))
	episode, _ := strconv.Atoi(r.URL.Query().Get("episode"))
	if id == 0 || episode <= 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "id and episode are required"})
		return
	}
	items, err := s.store.UpNextAfter(r.Context(), id, episode)
	if err != nil {
		s.fail(w, "up next after", err)
		return
	}
	out := map[string]any{"fromList": len(items) > 0}
	if len(items) > 0 {
		out["next"] = items[0]
	}
	if len(items) > 1 {
		out["then"] = items[1]
	}
	send(w, http.StatusOK, out)
}
