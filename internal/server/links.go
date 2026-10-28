package server

import (
	"net/http"
	"strconv"
)

// animeFromMal opens a shared MyAnimeList page: its show's AniList id, from what Yoru knows, else AniList's.
func (s *Server) animeFromMal(w http.ResponseWriter, r *http.Request) {
	mal, _ := strconv.Atoi(r.URL.Query().Get("id"))
	if mal <= 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "id is a MyAnimeList anime id"})
		return
	}
	id := s.store.AniListForMal(r.Context(), mal)
	if id == 0 {
		var err error
		if id, err = s.anilist.IDForMal(r.Context(), mal); err != nil {
			s.fail(w, "anime from mal", err)
			return
		}
	}
	if id == 0 {
		send(w, http.StatusNotFound, map[string]any{"error": "AniList has no anime for that MyAnimeList page."})
		return
	}
	send(w, http.StatusOK, map[string]any{"id": id})
}
