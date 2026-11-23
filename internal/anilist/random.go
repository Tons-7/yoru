package anilist

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"time"
)

const popularityFloor = 5000

const randomQuery = `query Random($page: Int!, $format: MediaFormat, $genres: [String], $year: Int, $isAdult: Boolean, $floor: Int) {
  Page(page: $page, perPage: 1) {
    pageInfo { total }
    media(type: ANIME, isAdult: $isAdult, popularity_greater: $floor, format: $format, genre_in: $genres, seasonYear: $year, sort: POPULARITY_DESC) {` + mediaFields + `}
  }
}`

type totals struct {
	mu sync.Mutex
	by map[string]total
}

type total struct {
	n  int
	at time.Time
}

const noMatchFor = 10 * time.Minute

var errNoMatch = fmt.Errorf("anilist: no anime matched")

func (t *totals) get(key string) (n int, known bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	c, ok := t.by[key]
	if !ok || (c.n == 0 && time.Since(c.at) > noMatchFor) {
		return 0, false
	}
	return c.n, true
}

func (t *totals) set(key string, n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.by == nil {
		t.by = map[string]total{}
	}
	t.by[key] = total{n: n, at: time.Now()}
}

func (c *Client) Random(ctx context.Context, format string, genres []string, year int) (Media, error) {
	vars := map[string]any{
		"floor":   popularityFloor,
		"isAdult": hasGenre(genres, "Hentai"),
	}
	setString(vars, "format", strings.ToUpper(format))
	setList(vars, "genres", genres)
	if year > 0 {
		vars["year"] = year
	}
	if hasGenre(genres, "Hentai") {
		vars["floor"] = 0
	}
	key := fmt.Sprint(format, "|", strings.Join(genres, ","), "|", year)

	bound, known := c.totals.get(key)
	if known && bound == 0 {
		return Media{}, errNoMatch
	}
	if !known {
		bound = 500
	}

	for range 8 {
		var out struct {
			Page struct {
				PageInfo struct {
					Total int `json:"total"`
				} `json:"pageInfo"`
				Media []Media `json:"media"`
			} `json:"Page"`
		}

		page := rand.IntN(bound) + 1
		vars["page"] = page
		if err := c.Query(ownAnswer(ctx), randomQuery, vars, &out); err != nil {
			return Media{}, err
		}

		if len(out.Page.Media) > 0 {
			if t := out.Page.PageInfo.Total; t > 0 && (!known || t < bound) {
				bound = t
			}
			c.totals.set(key, bound)
			return out.Page.Media[0], nil
		}
		if page == 1 || out.Page.PageInfo.Total == 0 {
			c.totals.set(key, 0)
			break
		}
		bound, known = page-1, true
		if t := out.Page.PageInfo.Total; t < bound {
			bound = t
		}
		c.totals.set(key, bound)
	}
	return Media{}, errNoMatch
}
