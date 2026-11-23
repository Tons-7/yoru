package anilist

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	SortTrending  = "trending"
	SortWeek      = "week"
	SortMonth     = "month"
	SortPopular   = "popular"
	SortSeason    = "season"
	SortTopRated  = "top"
	SortUpcoming  = "upcoming"
	SortFavourite = "favourites"
)

func RisingWindow(sort string) (int, bool) {
	switch strings.ToLower(sort) {
	case SortWeek:
		return 7, true
	case SortMonth:
		return 30, true
	}
	return 0, false
}

var sorts = map[string]string{
	SortTrending:  "TRENDING_DESC",
	SortPopular:   "POPULARITY_DESC",
	SortSeason:    "POPULARITY_DESC",
	SortTopRated:  "SCORE_DESC",
	SortUpcoming:  "POPULARITY_DESC",
	SortFavourite: "FAVOURITES_DESC",
}

func Sorts() []string {
	return []string{
		SortTrending, SortWeek, SortMonth, SortPopular,
		SortSeason, SortUpcoming, SortTopRated, SortFavourite,
	}
}

func Season(t time.Time) (string, int) {
	switch t.Month() {
	case time.January, time.February, time.March:
		return "WINTER", t.Year()
	case time.April, time.May, time.June:
		return "SPRING", t.Year()
	case time.July, time.August, time.September:
		return "SUMMER", t.Year()
	default:
		return "FALL", t.Year()
	}
}

func NextSeason(t time.Time) (string, int) {
	season, year := Season(t)
	switch season {
	case "WINTER":
		return "SPRING", year
	case "SPRING":
		return "SUMMER", year
	case "SUMMER":
		return "FALL", year
	default:
		return "WINTER", year + 1
	}
}

type DiscoverPage struct {
	Media       []Media
	HasNextPage bool
	Total       int
	Dropped     int
}

func discoverQuery(filters string, sort string) string {
	return fmt.Sprintf(`query Discover($page: Int!, $perPage: Int!) {
  Page(page: $page, perPage: $perPage) {
    pageInfo { total hasNextPage }
    media(type: ANIME, isAdult: false, sort: %s%s) {%s}
  }
}`, sort, filters, mediaFields)
}

func (c *Client) Discover(ctx context.Context, sort string, page, perPage int) (DiscoverPage, error) {
	order, ok := sorts[strings.ToLower(sort)]
	if !ok {
		return DiscoverPage{}, fmt.Errorf("anilist: unknown sort %q", sort)
	}
	if page <= 0 {
		page = 1
	}
	if perPage <= 0 || perPage > 50 {
		perPage = 30
	}

	var filters string
	switch strings.ToLower(sort) {
	case SortSeason:
		season, year := Season(time.Now())
		filters = fmt.Sprintf(", season: %s, seasonYear: %d", season, year)
	case SortUpcoming:
		season, year := NextSeason(time.Now())
		filters = fmt.Sprintf(", season: %s, seasonYear: %d", season, year)
	case SortTopRated:
		filters = ", popularity_greater: 5000"
	}

	var out struct {
		Page struct {
			PageInfo struct {
				Total       int  `json:"total"`
				HasNextPage bool `json:"hasNextPage"`
			} `json:"pageInfo"`
			Media []Media `json:"media"`
		} `json:"Page"`
	}
	err := c.Query(ctx, discoverQuery(filters, order),
		map[string]any{"page": page, "perPage": perPage}, &out)
	if err != nil {
		return DiscoverPage{}, err
	}

	return DiscoverPage{
		Media:       out.Page.Media,
		HasNextPage: out.Page.PageInfo.HasNextPage,
		Total:       out.Page.PageInfo.Total,
	}, nil
}
