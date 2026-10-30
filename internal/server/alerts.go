package server

import (
	"cmp"
	"net/http"
	"slices"
	"time"

	"yoru/internal/store"
)

func (s *Server) alertShows(w http.ResponseWriter, r *http.Request) {
	shows, err := s.store.AlertShows(r.Context())
	if err != nil {
		s.fail(w, "alert shows", err)
		return
	}
	global, err := s.store.Prefs(r.Context(), 0)
	if err != nil {
		s.fail(w, "alert prefs", err)
		return
	}
	enabled := global.String("notify.enabled") != "false"
	now := time.Now().Unix()
	upcoming := func(a store.AlertShow) bool { return a.NextAiringAt > now }
	slices.SortStableFunc(shows, func(a, b store.AlertShow) int {
		switch {
		case upcoming(a) && upcoming(b):
			return cmp.Compare(a.NextAiringAt, b.NextAiringAt)
		case upcoming(a):
			return -1
		case upcoming(b):
			return 1
		}
		return cmp.Compare(a.Title, b.Title)
	})
	send(w, http.StatusOK, map[string]any{"enabled": enabled, "shows": shows})
}

type notificationView struct {
	store.Notification
	Show  string  `json:"show,omitempty"`
	Cover *string `json:"cover,omitempty"`
}

func (s *Server) withShows(r *http.Request, items []store.Notification) []notificationView {
	var ids []int
	for _, n := range items {
		if n.AnimeID != nil && !slices.Contains(ids, *n.AnimeID) {
			ids = append(ids, *n.AnimeID)
		}
	}
	cards, err := s.store.ShowCards(r.Context(), ids)
	if err != nil {
		s.log.Warn("notification shows", "err", err)
	}
	out := make([]notificationView, len(items))
	for i, n := range items {
		out[i].Notification = n
		if n.AnimeID != nil {
			card := cards[*n.AnimeID]
			out[i].Show, out[i].Cover = card.Title, card.Cover
		}
	}
	return out
}
