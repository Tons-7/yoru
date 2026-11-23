package anilist

import "context"

const recommendQuery = `query Recommend($id: Int!, $perPage: Int!) {
  Media(id: $id, type: ANIME) {
    genres
    recommendations(sort: RATING_DESC, perPage: $perPage) {
      nodes {
        rating
        mediaRecommendation {` + mediaFields + `}
      }
    }
  }
}`

type Recommendation struct {
	Rating int   `json:"rating"`
	Media  Media `json:"media"`
}

type Recommendations struct {
	Genres []string         `json:"genres"`
	Items  []Recommendation `json:"items"`
}

func (c *Client) Recommend(ctx context.Context, animeID, limit int) (Recommendations, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	var out struct {
		Media struct {
			Genres          []string `json:"genres"`
			Recommendations struct {
				Nodes []struct {
					Rating int    `json:"rating"`
					Media  *Media `json:"mediaRecommendation"`
				} `json:"nodes"`
			} `json:"recommendations"`
		} `json:"Media"`
	}
	vars := map[string]any{"id": animeID, "perPage": limit}
	if err := c.Query(ctx, recommendQuery, vars, &out); err != nil {
		if !c.Authenticated() || c.queryAnonymous(ctx, recommendQuery, vars, &out) != nil {
			return Recommendations{}, err
		}
	}

	rec := Recommendations{Genres: out.Media.Genres}
	for _, n := range out.Media.Recommendations.Nodes {
		if n.Media == nil || n.Rating < 0 {
			continue
		}
		rec.Items = append(rec.Items, Recommendation{Rating: n.Rating, Media: *n.Media})
	}
	return rec, nil
}
