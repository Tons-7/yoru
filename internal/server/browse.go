package server

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"yoru/internal/anilist"
	"yoru/internal/store"
)

func csv(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func browseFilter(q url.Values) anilist.Browse {
	num := func(key string) int { n, _ := strconv.Atoi(q.Get(key)); return n }
	return anilist.Browse{
		Search:        q.Get("q"),
		Genres:        csv(q.Get("genres")),
		ExcludeGenres: csv(q.Get("excludeGenres")),
		Tags:          csv(q.Get("tags")),
		Year:          num("year"),
		Season:        q.Get("season"),
		Formats:       csv(q.Get("formats")),
		Statuses:      csv(q.Get("statuses")),
		MinScore:      num("minScore"),
		MinEpisodes:   num("minEpisodes"),
		MaxEpisodes:   num("maxEpisodes"),
		Country:       q.Get("country"),
		Source:        q.Get("source"),
		Sort:          q.Get("sort"),
		Page:          num("page"),
		PerPage:       num("perPage"),
	}
}

func (s *Server) browse(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if studio, _ := strconv.Atoi(q.Get("studio")); studio > 0 {
		s.browseStudio(w, r, studio)
		return
	}

	filter := browseFilter(q)
	result, err := s.anilist.BrowseMedia(r.Context(), filter)
	if err != nil {
		s.fail(w, "browse", err)
		return
	}

	send(w, http.StatusOK, map[string]any{
		"items":   s.decorate(r, result.Media),
		"page":    max(filter.Page, 1),
		"hasMore": result.HasNextPage,
	})
}

func (s *Server) browseCount(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := browseFilter(q)
	key := countKey(q)

	n, err := s.counts.get(key, func() (int, bool, error) {
		known := max(filter.Page, 1)*max(filter.PerPage, 1) + 1
		n, err := s.anilist.BrowseCount(r.Context(), filter, known)
		return n, !anilist.UsedSaved(r.Context()), err
	})
	if errors.Is(err, anilist.ErrBusy) || anilist.Unreachable(r.Context(), err) {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": err.Error(), "busy": true})
		return
	}
	if err != nil {
		s.fail(w, "browse count", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"count": n, "capped": n >= anilist.BrowseCap})
}

func countKey(q url.Values) string {
	k := url.Values{}
	for name, v := range q {
		if name != "page" && name != "perPage" {
			k[name] = v
		}
	}
	return k.Encode()
}

type browseCounts struct {
	mu      sync.Mutex
	done    map[string]countEntry
	running map[string]chan struct{}
}

type countEntry struct {
	n  int
	at time.Time
}

const countTTL = 30 * time.Minute

func (c *browseCounts) get(key string, measure func() (n int, keep bool, err error)) (int, error) {
	for {
		c.mu.Lock()
		if e, ok := c.done[key]; ok && time.Since(e.at) < countTTL {
			c.mu.Unlock()
			return e.n, nil
		}
		wait, busy := c.running[key]
		if !busy {
			if c.running == nil {
				c.running, c.done = map[string]chan struct{}{}, map[string]countEntry{}
			}
			wait = make(chan struct{})
			c.running[key] = wait
		}
		c.mu.Unlock()
		if busy {
			<-wait
			continue
		}

		n, keep, err := measure()
		c.mu.Lock()
		delete(c.running, key)
		if err == nil && keep {
			c.done[key] = countEntry{n: n, at: time.Now()}
		}
		c.mu.Unlock()
		close(wait)
		return n, err
	}
}

func (s *Server) studios(w http.ResponseWriter, r *http.Request) {
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(search) < 2 {
		send(w, http.StatusOK, map[string]any{"studios": []anilist.Studio{}})
		return
	}
	found, err := s.anilist.SearchStudios(r.Context(), search)
	if err != nil {
		s.fail(w, "studios", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"studios": found})
}

func containsFold(list []string, v string) bool {
	for _, x := range list {
		if strings.EqualFold(x, v) {
			return true
		}
	}
	return false
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func hasAll(have, want []string) bool {
	for _, w := range want {
		if !containsFold(have, w) {
			return false
		}
	}
	return true
}

func (s *Server) remember(r *http.Request, media []anilist.Media) {
	if s.importer != nil {
		s.importer.Remember(r.Context(), media)
	}
}

func (s *Server) decorate(r *http.Request, media []anilist.Media) []discoverItem {
	s.remember(r, media)
	onList, err := s.store.ListEntries(r.Context())
	if err != nil {
		s.log.Warn("list entries", "err", err)
	}
	titles := s.store.TitleMode(r.Context(), 0)
	var soon []int
	for _, m := range media {
		if m.Status != nil && *m.Status == "NOT_YET_RELEASED" {
			soon = append(soon, m.ID)
		}
	}
	alerts, err := s.store.FollowsOf(r.Context(), soon)
	if err != nil {
		s.log.Warn("follows", "err", err)
	}

	items := make([]discoverItem, 0, len(media))
	for _, m := range media {
		entry, listed := onList[m.ID]

		it := discoverItem{
			ID:          m.ID,
			English:     m.Title.English,
			Cover:       m.CoverImage.Large,
			CoverLarge:  m.CoverImage.ExtraLarge,
			Thumb:       m.CoverImage.Medium,
			Banner:      m.BannerImage,
			Color:       m.CoverImage.Color,
			Format:      m.Format,
			Status:      m.Status,
			Episodes:    m.Episodes,
			Season:      m.Season,
			SeasonYear:  m.SeasonYear,
			Score:       m.AverageScore,
			Popularity:  m.Popularity,
			Genres:      m.Genres,
			MalID:       m.IDMal,
			StartDate:   isoDate(m.StartDate),
			EndDate:     isoDate(m.EndDate),
			Duration:    m.Duration,
			Description: m.Description,
			OnList:      listed,
			Progress:    entry.Progress,
		}
		if listed && entry.Status != "" {
			status := entry.Status
			it.ListStatus = &status
		}
		if m.Title.Romaji != nil {
			it.Romaji = *m.Title.Romaji
		}
		if m.Status != nil && *m.Status == "NOT_YET_RELEASED" && alerts != nil {
			on := alerts[m.ID]
			it.AlertOn = &on
		}
		if m.NextAiring != nil {
			it.NextEpisode, it.NextAiringAt = m.NextAiring.Episode, int64(m.NextAiring.AiringAt)
		}
		for _, n := range m.Studios.Nodes {
			it.Studios = append(it.Studios, anilist.Studio{ID: n.ID, Name: n.Name})
		}
		it.Title = store.PickTitle(titles, it.Romaji, it.English)
		items = append(items, it)
	}
	return items
}

func (s *Server) recommend(w http.ResponseWriter, r *http.Request) {
	animeID, _ := strconv.Atoi(r.URL.Query().Get("anime"))
	if animeID <= 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "anime is required"})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	rec, err := s.anilist.Recommend(r.Context(), animeID, limit)
	if err != nil {
		s.fail(w, "recommend", err)
		return
	}

	media := make([]anilist.Media, 0, len(rec.Items))
	for _, item := range rec.Items {
		media = append(media, item.Media)
	}
	source := "community"

	if len(media) == 0 && len(rec.Genres) > 0 {
		source = "genre"
		fallback, err := s.anilist.BrowseMedia(r.Context(), anilist.Browse{
			Genres: rec.Genres, Sort: "popular", PerPage: 20,
		})
		if err != nil {
			s.fail(w, "recommend fallback", err)
			return
		}
		for _, m := range fallback.Media {
			if m.ID != animeID {
				media = append(media, m)
			}
		}
	}

	send(w, http.StatusOK, map[string]any{
		"items":  s.decorate(r, media),
		"source": source,
		"genres": rec.Genres,
	})
}

type vocabulary struct {
	mu      sync.Mutex
	genres  []string
	tags    []anilist.TagOption
	fetched time.Time
}

func (s *Server) filters(w http.ResponseWriter, r *http.Request) {
	s.vocab.mu.Lock()
	stale := time.Since(s.vocab.fetched) > 24*time.Hour || len(s.vocab.genres) == 0
	genres, tags := s.vocab.genres, s.vocab.tags
	s.vocab.mu.Unlock()

	if stale {
		fresh, freshTags, err := s.anilist.Vocabulary(r.Context())
		if err != nil {

			if len(genres) == 0 {
				s.fail(w, "filters", err)
				return
			}
		} else {
			genres, tags = fresh, freshTags

			if !anilist.UsedSaved(r.Context()) {
				s.vocab.mu.Lock()
				s.vocab.genres, s.vocab.tags, s.vocab.fetched = fresh, freshTags, time.Now()
				s.vocab.mu.Unlock()
			}
		}
	}

	season, year := anilist.Season(time.Now())
	years := make([]int, 0, year-1939)
	for y := year + 1; y >= 1940; y-- {
		years = append(years, y)
	}

	send(w, http.StatusOK, map[string]any{
		"genres":   genres,
		"tags":     tags,
		"formats":  anilist.Formats,
		"statuses": anilist.Statuses,
		"seasons":  anilist.Seasons,
		"sources":  anilist.Sources,
		"sorts":    anilist.BrowseSorts(),
		"years":    years,
		"current":  map[string]any{"season": season, "year": year},
	})
}
