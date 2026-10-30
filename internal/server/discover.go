package server

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"yoru/internal/anilist"
)

type discoverItem struct {
	ID      int     `json:"id"`
	Title   string  `json:"title"`
	Romaji  string  `json:"romaji"`
	English *string `json:"english,omitempty"`

	Cover      *string `json:"cover,omitempty"`
	CoverLarge *string `json:"coverLarge,omitempty"`
	Thumb      *string `json:"thumb,omitempty"`
	Banner     *string `json:"banner,omitempty"`
	Color      *string `json:"color,omitempty"`
	Format     *string `json:"format,omitempty"`
	Status     *string `json:"status,omitempty"`
	Episodes   *int    `json:"episodes,omitempty"`
	Season     *string `json:"season,omitempty"`
	SeasonYear *int    `json:"seasonYear,omitempty"`
	Score      *int    `json:"score,omitempty"`
	Popularity *int    `json:"popularity,omitempty"`
	Genres     []string

	Description *string `json:"description,omitempty"`
	OnList      bool    `json:"onList"`
	Progress    int     `json:"progress"`

	ListStatus *string `json:"listStatus,omitempty"`
	AlertOn    *bool   `json:"alertOn,omitempty"`

	MalID        *int             `json:"malId,omitempty"`
	NextEpisode  int              `json:"nextEpisode,omitempty"`
	NextAiringAt int64            `json:"nextAiringAt,omitempty"`
	StartDate    string           `json:"startDate,omitempty"`
	EndDate      string           `json:"endDate,omitempty"`
	Duration     *int             `json:"duration,omitempty"`
	Studios      []anilist.Studio `json:"studios,omitempty"`
}

func isoDate(d anilist.FuzzyDate) string {
	switch {
	case d.Year == nil:
		return ""
	case d.Month == nil:
		return strconv.Itoa(*d.Year)
	case d.Day == nil:
		return fmt.Sprintf("%04d-%02d", *d.Year, *d.Month)
	}
	return fmt.Sprintf("%04d-%02d-%02d", *d.Year, *d.Month, *d.Day)
}

func (s *Server) WarmRankings(ctx context.Context) {
	for {
		for _, sort := range []string{anilist.SortWeek, anilist.SortMonth} {
			days, _ := anilist.RisingWindow(sort)
			_, err := s.rising.compute(sort, func() (anilist.DiscoverPage, error) {
				result, err := s.anilist.Rising(ctx, days, 30)
				if err == nil {
					s.rising.put(sort, result)
				}
				return result, err
			})
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				s.log.Warn("warm ranking", "sort", sort, "err", err)
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(risingTTL):
		}
	}
}

func (s *Server) ranked(r *http.Request, sort string, page, perPage int) (anilist.DiscoverPage, error) {
	days, computed := anilist.RisingWindow(sort)
	if !computed {
		return s.anilist.Discover(r.Context(), sort, page, perPage)
	}

	if hit, ok := s.rising.get(sort); ok {
		return hit, nil
	}
	return s.rising.compute(sort, func() (anilist.DiscoverPage, error) {
		result, err := s.anilist.Rising(r.Context(), days, max(perPage, 30))
		if err == nil && !anilist.UsedSaved(r.Context()) {
			s.rising.put(sort, result)
		}
		return result, err
	})
}

type risingCache struct {
	mu     sync.Mutex
	page   map[string]risingEntry
	flight singleflight.Group
}

func (c *risingCache) compute(key string, fetch func() (anilist.DiscoverPage, error)) (anilist.DiscoverPage, error) {
	v, err, _ := c.flight.Do(key, func() (any, error) { return fetch() })
	if err != nil {
		return anilist.DiscoverPage{}, err
	}
	return v.(anilist.DiscoverPage), nil
}

type risingEntry struct {
	result anilist.DiscoverPage
	at     time.Time
}

const risingTTL = 3 * time.Hour

func (c *risingCache) get(key string) (anilist.DiscoverPage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.page[key]
	if !ok || time.Since(e.at) > risingTTL+10*time.Minute {
		return anilist.DiscoverPage{}, false
	}
	return e.result, true
}

func (c *risingCache) put(key string, result anilist.DiscoverPage) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.page == nil {
		c.page = map[string]risingEntry{}
	}
	c.page[key] = risingEntry{result: result, at: time.Now()}
}

func (s *Server) discover(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	sort := q.Get("sort")
	if sort == "" {
		sort = anilist.SortTrending
	}
	page, _ := strconv.Atoi(q.Get("page"))
	perPage, _ := strconv.Atoi(q.Get("perPage"))

	if !slices.Contains(anilist.Sorts(), sort) {
		send(w, http.StatusBadRequest, map[string]any{"error": "unknown sort " + sort, "sorts": anilist.Sorts()})
		return
	}
	result, err := s.ranked(r, sort, page, perPage)
	if err != nil {
		s.fail(w, "discover", err)
		return
	}

	items := s.decorate(r, result.Media)

	if page <= 0 {
		page = 1
	}
	season, year := anilist.Season(time.Now())

	send(w, http.StatusOK, map[string]any{
		"items":   items,
		"sort":    sort,
		"page":    page,
		"hasMore": result.HasNextPage,
		"total":   result.Total,
		"sorts":   anilist.Sorts(),
		"season":  map[string]any{"season": season, "year": year},
	})
}

func (s *Server) anilistRecheck(w http.ResponseWriter, r *http.Request) {
	if err := s.anilist.Recheck(r.Context()); err != nil {
		send(w, http.StatusOK, map[string]any{"live": false, "error": err.Error()})
		return
	}
	send(w, http.StatusOK, map[string]any{"live": true})
}
