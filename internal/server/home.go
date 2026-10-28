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

// mediaInOrder is AniList's answer for these shows, in the order asked: AniList returns its own. Partial is
// fine: a show hidden from the account is only left out.
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

// forYouPicks is the day's "For you", kept for the shows it grew from: AniList's community picks move slowly.
type forYouPicks struct {
	at    time.Time
	seeds string
	ids   []int
}

// homeForYou is Home's "For you": what AniList's community recommends after the viewer's best-liked shows,
// weighted by each one's place among them and the recommendation's rating, none already known (store.Known).
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
	// One added or favourited since the day's picks were made leaves at once.
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

// forYouIDs are the day's picks for these seeds, asked once (one request for all ten); adult ones if allowed.
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
		// The best-liked show counts in full, falling to about half for the tenth.
		weight[rec.ID] += float64(rec.Rating) * (1 - float64(place[rec.From])/float64(2*len(seeds)))
	}
	ids := slices.Collect(maps.Keys(weight))
	slices.SortFunc(ids, func(a, b int) int { return cmp.Or(cmp.Compare(weight[b], weight[a]), a-b) })
	ids = ids[:min(len(ids), 20)]
	s.forYouMu.Lock()
	s.forYou = forYouPicks{at: time.Now(), seeds: key, ids: ids}
	s.forYouMu.Unlock()
	return ids, nil
}

// homeSequels is Home's "Sequels you missed": what follows shows finished on the list, not yet started.
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
	// By AniList's word now: a sequel saved before it was dated may not be out yet.
	media = slices.DeleteFunc(media, func(m anilist.Media) bool {
		return m.Status == nil || (*m.Status != "FINISHED" && *m.Status != "RELEASING")
	})
	send(w, http.StatusOK, map[string]any{"items": s.decorate(r, media)})
}
