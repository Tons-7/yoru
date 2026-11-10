package server

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"time"

	"yoru/internal/anilist"
)

func (s *Server) mediaInOrder(ctx context.Context, ids []int) ([]anilist.Media, error) {
	media, err := s.anilist.MediaByIDs(ctx, ids)
	if err != nil && !errors.Is(err, anilist.ErrIncomplete) {
		return nil, err
	}
	at := make(map[int]int, len(ids))
	for i, id := range ids {
		at[id] = i
	}
	slices.SortFunc(media, func(a, b anilist.Media) int { return at[a.ID] - at[b.ID] })
	return media, nil
}

type forYouPicks struct {
	at    time.Time
	seeds string
	ids   []int
}

func (s *Server) homeForYou(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	seeds, err := s.store.TopScored(ctx, 10)
	if err != nil {
		s.fail(w, "for you", err)
		return
	}
	prefs, err := s.store.Prefs(ctx, 0)
	if err != nil {
		s.fail(w, "for you", err)
		return
	}
	ids, err := s.forYouIDs(ctx, seeds, prefs.Bool("home.adult_picks"))
	if err != nil {
		s.fail(w, "for you", err)
		return
	}

	known, err := s.store.Known(ctx)
	if err != nil {
		s.fail(w, "for you", err)
		return
	}
	ids = slices.DeleteFunc(slices.Clone(ids), func(id int) bool { return known[id] })
	media, err := s.mediaInOrder(ctx, ids)
	if err != nil {
		s.fail(w, "for you", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"items": s.decorate(r, media)})
}

func (s *Server) forYouIDs(ctx context.Context, seeds []int, adult bool) ([]int, error) {
	key := fmt.Sprint(seeds, adult)
	s.forYouMu.Lock()
	cached := s.forYou
	s.forYouMu.Unlock()
	if cached.seeds == key && time.Since(cached.at) < 24*time.Hour {
		return cached.ids, nil
	}
	if len(seeds) == 0 {
		return nil, nil
	}
	recs, err := s.anilist.Recommendations(ctx, seeds, 10)
	if err != nil {
		return nil, err
	}
	known, err := s.store.Known(ctx)
	if err != nil {
		return nil, err
	}
	place := make(map[int]int, len(seeds))
	for i, id := range seeds {
		place[id] = i
	}
	weight := map[int]float64{}
	for _, rec := range recs {
		if known[rec.ID] || rec.Rating <= 0 || rec.Adult && !adult {
			continue
		}

		weight[rec.ID] += float64(rec.Rating) * (1 - float64(place[rec.From])/float64(2*len(seeds)))
	}
	ids := slices.Collect(maps.Keys(weight))
	slices.SortFunc(ids, func(a, b int) int { return cmp.Or(cmp.Compare(weight[b], weight[a]), a-b) })
	ids = ids[:min(len(ids), 20)]
	if !anilist.UsedSaved(ctx) {
		s.forYouMu.Lock()
		s.forYou = forYouPicks{at: time.Now(), seeds: key, ids: ids}
		s.forYouMu.Unlock()
	}
	return ids, nil
}

func (s *Server) homeSequels(w http.ResponseWriter, r *http.Request) {
	ids, err := s.store.SequelsMissed(r.Context(), 20)
	if err != nil {
		s.fail(w, "sequels", err)
		return
	}
	media, err := s.mediaInOrder(r.Context(), ids)
	if err != nil {
		s.fail(w, "sequels", err)
		return
	}

	media = slices.DeleteFunc(media, func(m anilist.Media) bool {
		return m.Status == nil || (*m.Status != "FINISHED" && *m.Status != "RELEASING")
	})
	send(w, http.StatusOK, map[string]any{"items": s.decorate(r, media)})
}
