package anilist

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

type Media struct {
	ID              int       `json:"id"`
	IDMal           *int      `json:"idMal"`
	Title           Title     `json:"title"`
	Format          *string   `json:"format"`
	Status          *string   `json:"status"`
	Episodes        *int      `json:"episodes"`
	Duration        *int      `json:"duration"`
	Season          *string   `json:"season"`
	SeasonYear      *int      `json:"seasonYear"`
	Description     *string   `json:"description"`
	Source          *string   `json:"source"`
	Genres          []string  `json:"genres"`
	Synonyms        []string  `json:"synonyms"`
	AverageScore    *int      `json:"averageScore"`
	MeanScore       *int      `json:"meanScore"`
	Popularity      *int      `json:"popularity"`
	Favourites      *int      `json:"favourites"`
	Trending        *int      `json:"trending"`
	CountryOfOrigin *string   `json:"countryOfOrigin"`
	IsAdult         bool      `json:"isAdult"`
	CoverImage      Cover     `json:"coverImage"`
	BannerImage     *string   `json:"bannerImage"`
	StartDate       FuzzyDate `json:"startDate"`
	EndDate         FuzzyDate `json:"endDate"`
	Studios         Studios   `json:"studios"`
	Tags            []Tag     `json:"tags"`
	Links           []Link    `json:"externalLinks"`
	Trailer         *Trailer  `json:"trailer"`
	NextAiring      *Airing   `json:"nextAiringEpisode"`
}

type Studios struct {
	Nodes []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"nodes"`
}

func (s Studios) Names() []string {
	out := make([]string, 0, len(s.Nodes))
	for _, n := range s.Nodes {
		out = append(out, n.Name)
	}
	return out
}

type Tag struct {
	Name           string `json:"name"`
	Rank           int    `json:"rank"`
	GeneralSpoiler bool   `json:"isGeneralSpoiler"`
	MediaSpoiler   bool   `json:"isMediaSpoiler"`
}

type Link struct {
	Site     string  `json:"site"`
	URL      string  `json:"url"`
	Type     *string `json:"type"`
	Language *string `json:"language"`
}

type Trailer struct {
	ID        *string `json:"id"`
	Site      *string `json:"site"`
	Thumbnail *string `json:"thumbnail"`
}

type Title struct {
	Romaji        *string `json:"romaji"`
	English       *string `json:"english"`
	Native        *string `json:"native"`
	UserPreferred *string `json:"userPreferred"`
}

type Cover struct {
	ExtraLarge *string `json:"extraLarge"`
	Large      *string `json:"large"`
	Medium     *string `json:"medium"`
	Color      *string `json:"color"`
}

type Airing struct {
	Episode         int `json:"episode"`
	AiringAt        int `json:"airingAt"`
	TimeUntilAiring int `json:"timeUntilAiring"`
}

type page struct {
	Page struct {
		PageInfo struct {
			HasNextPage bool `json:"hasNextPage"`
		} `json:"pageInfo"`
		Media []Media `json:"media"`
	} `json:"Page"`
}

const mediaFields = `
  id idMal isAdult countryOfOrigin
  title { romaji english native userPreferred }
  format status episodes duration season seasonYear
  averageScore meanScore popularity favourites trending genres synonyms
  description(asHtml: false)
  source
  coverImage { extraLarge large medium color }
  bannerImage
  startDate { year month day }
  endDate { year month day }
  studios(isMain: true) { nodes { id name } }
  tags { name rank isGeneralSpoiler isMediaSpoiler }
  externalLinks { site url type language }
  trailer { id site thumbnail }
  nextAiringEpisode { episode airingAt timeUntilAiring }
`

const searchQuery = `query Search($q: String!, $perPage: Int!) {
  Page(page: 1, perPage: $perPage) {
    pageInfo { hasNextPage }
    media(search: $q, type: ANIME, sort: SEARCH_MATCH) {` + mediaFields + `}
  }
}`

func (c *Client) Search(ctx context.Context, q string, limit int) ([]Media, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	var out page
	err := c.Query(ctx, searchQuery, map[string]any{"q": q, "perPage": limit}, &out)
	return out.Page.Media, err
}

const seasonQuery = `query Season($season: MediaSeason!, $year: Int!, $perPage: Int!) {
  Page(page: 1, perPage: $perPage) {
    pageInfo { hasNextPage }
    media(season: $season, seasonYear: $year, type: ANIME, sort: POPULARITY_DESC) {` + mediaFields + `}
  }
}`

const relationsQuery = `query Relations($ids: [Int]!) {
  Page(page: 1, perPage: 25) {
    media(id_in: $ids, type: ANIME) {
      id
      episodes
      startDate { year month day }
      relations {
        edges {
          relationType(version: 2)
          node { id type format status episodes startDate { year month day } }
        }
      }
    }
  }
}`

type RelatedMedia struct {
	ID        int       `json:"id"`
	Episodes  *int      `json:"episodes"`
	StartDate FuzzyDate `json:"startDate"`
	Relations struct {
		Edges []RelationEdge `json:"edges"`
	} `json:"relations"`
}

type RelationEdge struct {
	Type string `json:"relationType"`
	Node struct {
		ID        int       `json:"id"`
		Type      string    `json:"type"`
		Format    *string   `json:"format"`
		Status    *string   `json:"status"`
		Episodes  *int      `json:"episodes"`
		StartDate FuzzyDate `json:"startDate"`
	} `json:"node"`
}

func (c *Client) Relations(ctx context.Context, ids []int) ([]RelatedMedia, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > 25 {
		ids = ids[:25]
	}
	var out struct {
		Page struct {
			Media []RelatedMedia `json:"media"`
		} `json:"Page"`
	}
	err := c.Query(ctx, relationsQuery, map[string]any{"ids": ids}, &out)
	return out.Page.Media, err
}

const scheduleQuery = `query Schedule($start: Int!, $end: Int!, $page: Int!) {
  Page(page: $page, perPage: 50) {
    pageInfo { hasNextPage }
    airingSchedules(airingAt_greater: $start, airingAt_lesser: $end, sort: TIME) {
      id episode airingAt timeUntilAiring mediaId
      media {` + mediaFields + `}
    }
  }
}`

type ScheduleEntry struct {
	ID              int   `json:"id"`
	Episode         int   `json:"episode"`
	AiringAt        int   `json:"airingAt"`
	TimeUntilAiring int   `json:"timeUntilAiring"`
	MediaID         int   `json:"mediaId"`
	Media           Media `json:"media"`
}

func (c *Client) Schedule(ctx context.Context, start, end int64) ([]ScheduleEntry, error) {
	var out []ScheduleEntry

	for page := 1; page <= 20; page++ {
		var res struct {
			Page struct {
				PageInfo struct {
					HasNextPage bool `json:"hasNextPage"`
				} `json:"pageInfo"`
				Schedules []ScheduleEntry `json:"airingSchedules"`
			} `json:"Page"`
		}
		err := c.Query(ctx, scheduleQuery, map[string]any{
			"start": start, "end": end, "page": page,
		}, &res)
		if err != nil {
			return out, err
		}

		out = append(out, res.Page.Schedules...)
		if !res.Page.PageInfo.HasNextPage || len(res.Page.Schedules) == 0 {
			break
		}
	}
	return out, nil
}

const byIDsQuery = `query ByIDs($ids: [Int]!) {
  Page(page: 1, perPage: 50) {
    pageInfo { hasNextPage }
    media(id_in: $ids, type: ANIME) {` + mediaFields + `}
  }
}`

func (c *Client) MediaByIDs(ctx context.Context, ids []int) ([]Media, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > 50 {
		ids = ids[:50]
	}

	var out page
	err := c.Query(ctx, byIDsQuery, map[string]any{"ids": ids}, &out)
	if err != nil || !missingAny(ids, out.Page.Media) || !c.Authenticated() {
		return out.Page.Media, err
	}

	var anon page
	if err := c.queryAnonymous(ctx, byIDsQuery, map[string]any{"ids": ids}, &anon); err != nil {
		return out.Page.Media, fmt.Errorf("%w: %v", ErrIncomplete, err)
	}
	if len(anon.Page.Media) > len(out.Page.Media) {
		return anon.Page.Media, nil
	}
	return out.Page.Media, nil
}

type Recommended struct {
	From, ID, Rating int
	Adult            bool
}

const missingFor = 24 * time.Hour

func (c *Client) knownMissing(id int) bool {
	c.missingMu.Lock()
	defer c.missingMu.Unlock()
	at, ok := c.missing[id]
	if ok && time.Since(at) > missingFor {
		delete(c.missing, id)
		return false
	}
	return ok
}

func (c *Client) noteMissing(id int) {
	c.missingMu.Lock()
	defer c.missingMu.Unlock()
	if c.missing == nil {
		c.missing = map[int]time.Time{}
	}
	c.missing[id] = time.Now()
}

func (c *Client) Recommendations(ctx context.Context, ids []int, per int) ([]Recommended, error) {
	ids = slices.DeleteFunc(slices.Clone(ids), c.knownMissing)
	if len(ids) == 0 {
		return nil, nil
	}
	recs, err := c.recommendations(ctx, ids, per)
	errs, ok := errors.AsType[Errors](err)
	if !ok || !errs.NotFound() {
		return recs, err
	}
	if len(ids) < 2 {
		c.noteMissing(ids[0])
		return recs, err
	}
	recs = nil
	for _, id := range ids {
		one, err := c.recommendations(ctx, []int{id}, per)
		if errs, ok := errors.AsType[Errors](err); ok && errs.NotFound() {
			c.noteMissing(id)
			continue
		}
		if err != nil {
			return nil, err
		}
		recs = append(recs, one...)
	}
	return recs, nil
}

func (c *Client) recommendations(ctx context.Context, ids []int, per int) ([]Recommended, error) {
	var q strings.Builder
	q.WriteString("query {")
	for i, id := range ids {
		fmt.Fprintf(&q, " m%d: Media(id: %d, type: ANIME) { id recommendations(sort: RATING_DESC, perPage: %d) "+
			"{ nodes { rating mediaRecommendation { id type isAdult } } } }", i, id, per)
	}
	q.WriteString(" }")
	var out map[string]*struct {
		ID              int `json:"id"`
		Recommendations struct {
			Nodes []struct {
				Rating              int `json:"rating"`
				MediaRecommendation *struct {
					ID      int    `json:"id"`
					Type    string `json:"type"`
					IsAdult bool   `json:"isAdult"`
				} `json:"mediaRecommendation"`
			} `json:"nodes"`
		} `json:"recommendations"`
	}
	if err := c.Query(ctx, q.String(), nil, &out); err != nil {
		return nil, err
	}
	var recs []Recommended
	for _, m := range out {
		if m == nil {
			continue
		}
		for _, n := range m.Recommendations.Nodes {
			if r := n.MediaRecommendation; r != nil && r.Type == "ANIME" {
				recs = append(recs, Recommended{From: m.ID, ID: r.ID, Rating: n.Rating, Adult: r.IsAdult})
			}
		}
	}
	return recs, nil
}

func (c *Client) IDForMal(ctx context.Context, malID int) (int, error) {
	var out struct {
		Media *struct {
			ID int `json:"id"`
		} `json:"Media"`
	}
	const query = `query ($mal: Int) { Media(idMal: $mal, type: ANIME) { id } }`
	vars := map[string]any{"mal": malID}
	err := c.Query(ctx, query, vars, &out)
	if errs, ok := errors.AsType[Errors](err); ok && errs.NotFound() {
		if !c.Authenticated() || c.queryAnonymous(ctx, query, vars, &out) != nil || out.Media == nil {
			return 0, nil
		}
		return out.Media.ID, nil
	}
	if err != nil || out.Media == nil {
		return 0, err
	}
	return out.Media.ID, nil
}

var ErrIncomplete = errors.New("anilist: some titles could not be checked")

func missingAny(ids []int, got []Media) bool {
	have := make(map[int]struct{}, len(got))
	for _, m := range got {
		have[m.ID] = struct{}{}
	}
	for _, id := range ids {
		if _, ok := have[id]; !ok {
			return true
		}
	}
	return false
}

type StreamingEpisode struct {
	Title     string `json:"title"`
	Thumbnail string `json:"thumbnail"`
	URL       string `json:"url"`
	Site      string `json:"site"`
}

const streamingQuery = `query Streaming($id: Int!) {
  Media(id: $id, type: ANIME) {
    streamingEpisodes { title thumbnail url site }
  }
}`

func (c *Client) StreamingEpisodes(ctx context.Context, id int) ([]StreamingEpisode, error) {
	var out struct {
		Media struct {
			StreamingEpisodes []StreamingEpisode `json:"streamingEpisodes"`
		} `json:"Media"`
	}
	if err := c.Query(ctx, streamingQuery, map[string]any{"id": id}, &out); err != nil {
		if c.queryAnonymous(ctx, streamingQuery, map[string]any{"id": id}, &out) != nil {
			return nil, err
		}
	}
	return out.Media.StreamingEpisodes, nil
}

func (c *Client) Season(ctx context.Context, season string, year, limit int) ([]Media, error) {
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	var out page
	err := c.Query(ctx, seasonQuery, map[string]any{"season": season, "year": year, "perPage": limit}, &out)
	return out.Page.Media, err
}
