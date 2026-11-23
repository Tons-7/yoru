package anilist

import (
	"context"
	"sort"
	"time"
)

const (
	trendPool     = 100
	trendPageSize = 50
	trendMaxPages = 4
	dayLength     = int64(24 * time.Hour / time.Second)
)

const trendQuery = `query Trends($ids: [Int], $since: Int, $page: Int!, $perPage: Int!) {
  Page(page: $page, perPage: $perPage) {
    pageInfo { hasNextPage }
    mediaTrends(mediaId_in: $ids, date_greater: $since, sort: DATE) {
      mediaId
      date
      popularity
    }
  }
}`

func (c *Client) popularityAt(ctx context.Context, ids []int, day time.Time) (map[int]int, error) {
	since := day.UTC().Truncate(24*time.Hour).Unix() - 1
	found := make(map[int]int, len(ids))

	for start := 0; start < len(ids); start += trendPageSize {
		batch := ids[start:min(start+trendPageSize, len(ids))]
		if err := c.readBatch(ctx, batch, since, found); err != nil {
			return nil, err
		}
	}
	return found, nil
}

func (c *Client) readBatch(ctx context.Context, ids []int, since int64, found map[int]int) error {
	dates := make(map[int]int64, len(ids))

	for page := 1; page <= trendMaxPages; page++ {
		var out struct {
			Page struct {
				PageInfo struct {
					HasNextPage bool `json:"hasNextPage"`
				} `json:"pageInfo"`
				MediaTrends []struct {
					MediaID    int   `json:"mediaId"`
					Date       int64 `json:"date"`
					Popularity int   `json:"popularity"`
				} `json:"mediaTrends"`
			} `json:"Page"`
		}

		err := c.Query(ctx, trendQuery, map[string]any{
			"ids": ids, "since": since, "page": page, "perPage": trendPageSize,
		}, &out)
		if err != nil {
			return err
		}

		pastDay := false
		for _, t := range out.Page.MediaTrends {
			if t.Date > since+dayLength {
				pastDay = true
				break
			}
			if at, seen := dates[t.MediaID]; !seen || t.Date < at {
				dates[t.MediaID] = t.Date
				found[t.MediaID] = t.Popularity
			}
		}
		if pastDay || len(dates) == len(ids) || !out.Page.PageInfo.HasNextPage ||
			len(out.Page.MediaTrends) == 0 {
			break
		}
	}
	return nil
}

func (c *Client) Rising(ctx context.Context, days, perPage int) (DiscoverPage, error) {
	if perPage <= 0 || perPage > trendPageSize {
		perPage = 30
	}

	var pool []Media
	for page := 1; len(pool) < trendPool; page++ {
		got, err := c.Discover(ctx, SortTrending, page, trendPageSize)
		if err != nil {
			return DiscoverPage{}, err
		}
		pool = append(pool, got.Media...)
		if !got.HasNextPage {
			break
		}
	}

	ids := make([]int, 0, len(pool))
	for _, m := range pool {
		ids = append(ids, m.ID)
	}

	baseline, err := c.popularityAt(ctx, ids, time.Now().AddDate(0, 0, -days))
	if err != nil {
		return DiscoverPage{}, err
	}

	ranked := rankByGain(pool, baseline)
	if len(ranked) > perPage {
		ranked = ranked[:perPage]
	}
	return DiscoverPage{Media: ranked, Total: len(ranked)}, nil
}

func rankByGain(pool []Media, baseline map[int]int) []Media {
	gain := make(map[int]int, len(pool))
	out := make([]Media, 0, len(pool))

	for _, m := range pool {
		before, measured := baseline[m.ID]
		if !measured || m.Popularity == nil {
			continue
		}
		gain[m.ID] = *m.Popularity - before
		out = append(out, m)
	}

	sort.SliceStable(out, func(i, j int) bool {
		return gain[out[i].ID] > gain[out[j].ID]
	})
	return out
}
