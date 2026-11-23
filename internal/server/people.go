package server

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"

	"yoru/internal/metadata"
)

type peopleShow struct {
	MalID  int    `json:"malId"`
	ID     int    `json:"id,omitempty"`
	Title  string `json:"title"`
	Image  string `json:"image,omitempty"`
	Year   int    `json:"year,omitempty"`
	Status string `json:"status,omitempty"`
}

type played struct {
	Character metadata.Person `json:"character"`
	Role      string          `json:"role,omitempty"`
	Shows     []peopleShow    `json:"shows"`
}

type worked struct {
	Show      peopleShow `json:"show"`
	Positions []string   `json:"positions"`
}

func (s *Server) showsAsKnown(ctx context.Context, shown []metadata.Shown) (map[int]peopleShow, error) {
	ids := make([]int, 0, len(shown))
	for _, sh := range shown {
		ids = append(ids, sh.MalID)
	}
	known, err := s.store.ShowsForMal(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[int]peopleShow, len(shown))
	for _, sh := range shown {
		k := known[sh.MalID]
		out[sh.MalID] = peopleShow{MalID: sh.MalID, ID: k.ID, Title: cmp.Or(k.Title, sh.Title), Image: sh.Image,
			Year: k.Year, Status: k.Status}
	}
	return out, nil
}

func byListThenNewest(a, b peopleShow) int {
	listed := func(s peopleShow) int {
		if s.Status != "" {
			return 1
		}
		return 0
	}
	return cmp.Or(cmp.Compare(listed(b), listed(a)), cmp.Compare(b.Year, a.Year), cmp.Compare(b.MalID, a.MalID))
}

func (s *Server) person(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "bad person id"})
		return
	}
	if s.enricher == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "People pages aren't available right now."})
		return
	}
	p, err := s.enricher.Person(r.Context(), id)
	if errors.Is(err, metadata.ErrNoPerson) {
		send(w, http.StatusNotFound, map[string]any{"error": "MyAnimeList has no one by that number."})
		return
	}
	if err != nil {
		s.fail(w, "person", err)
		return
	}
	var shown []metadata.Shown
	for _, role := range p.Roles {
		shown = append(shown, role.Show)
	}
	for _, work := range p.Work {
		shown = append(shown, work.Show)
	}
	shows, err := s.showsAsKnown(r.Context(), shown)
	if err != nil {
		s.fail(w, "person", err)
		return
	}

	var characters []*played
	byID := map[int]*played{}
	for _, role := range p.Roles {
		c := byID[role.Character.ID]
		if c == nil {
			c = &played{Character: role.Character, Role: role.Role}
			byID[role.Character.ID] = c
			characters = append(characters, c)
		}
		if role.Role == "Main" {
			c.Role = "Main"
		}
		c.Shows = append(c.Shows, shows[role.Show.MalID])
	}
	for _, c := range characters {
		slices.SortFunc(c.Shows, byListThenNewest)
	}
	main := func(c *played) int {
		if c.Role == "Main" {
			return 1
		}
		return 0
	}
	slices.SortStableFunc(characters, func(a, b *played) int {
		listed := func(c *played) int {
			if c.Shows[0].Status != "" {
				return 1
			}
			return 0
		}
		return cmp.Or(cmp.Compare(listed(b), listed(a)), cmp.Compare(main(b), main(a)), byListThenNewest(a.Shows[0], b.Shows[0]))
	})

	var work []*worked
	byShow := map[int]*worked{}
	for _, wk := range p.Work {
		row := byShow[wk.Show.MalID]
		if row == nil {
			row = &worked{Show: shows[wk.Show.MalID]}
			byShow[wk.Show.MalID] = row
			work = append(work, row)
		}
		if !slices.Contains(row.Positions, wk.Position) {
			row.Positions = append(row.Positions, wk.Position)
		}
	}
	slices.SortStableFunc(work, func(a, b *worked) int { return byListThenNewest(a.Show, b.Show) })

	send(w, http.StatusOK, map[string]any{
		"id": p.ID, "name": p.Name, "native": p.Native, "image": p.Image, "about": p.About, "birthday": p.Birthday,
		"favorites": p.Favorites, "characters": orEmpty(characters), "work": orEmpty(work),
	})
}

func (s *Server) character(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "bad character id"})
		return
	}
	if s.enricher == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "People pages aren't available right now."})
		return
	}
	c, err := s.enricher.Character(r.Context(), id)
	if errors.Is(err, metadata.ErrNoPerson) {
		send(w, http.StatusNotFound, map[string]any{"error": "MyAnimeList has no character by that number."})
		return
	}
	if err != nil {
		s.fail(w, "character", err)
		return
	}
	shown := make([]metadata.Shown, 0, len(c.Appearances))
	for _, a := range c.Appearances {
		shown = append(shown, a.Show)
	}
	shows, err := s.showsAsKnown(r.Context(), shown)
	if err != nil {
		s.fail(w, "character", err)
		return
	}
	type appearance struct {
		Role string     `json:"role,omitempty"`
		Show peopleShow `json:"show"`
	}
	appearances := make([]appearance, 0, len(c.Appearances))
	for _, a := range c.Appearances {
		appearances = append(appearances, appearance{Role: a.Role, Show: shows[a.Show.MalID]})
	}
	slices.SortStableFunc(appearances, func(a, b appearance) int { return byListThenNewest(a.Show, b.Show) })
	send(w, http.StatusOK, map[string]any{
		"id": c.ID, "name": c.Name, "native": c.Native, "image": c.Image, "about": c.About, "favorites": c.Favorites,
		"appearances": appearances, "voices": c.Voices,
	})
}

func orEmpty[T any](list []T) []T {
	if list == nil {
		return []T{}
	}
	return list
}
