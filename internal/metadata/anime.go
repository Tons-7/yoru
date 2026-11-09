package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const requestWait = 4 * time.Second

var errNoRecord = errors.New("metadata: no such record")

func (c *Client) record(ctx context.Context, name, pattern string, malID int, found func([]byte) bool) error {
	rctx, cancel := context.WithTimeout(ctx, requestWait)
	defer cancel()
	body, err := c.get(rctx, c.url(name, fmt.Sprintf(pattern, malID)))
	if err != nil {
		return err
	}
	if !found(body) {
		return errNoRecord
	}
	return nil
}

const TenraiAnimeURL = "https://api.tenrai.org/v1/anime/%d"

type AnimeDetail struct {
	MalID       int      `json:"malId"`
	Romaji      string   `json:"romaji"`
	English     string   `json:"english"`
	Native      string   `json:"native"`
	Synonyms    []string `json:"synonyms"`
	Format      string   `json:"format"`
	Status      string   `json:"status"`
	Episodes    int      `json:"episodes"`
	Duration    string   `json:"duration"`
	Season      string   `json:"season"`
	Year        int      `json:"year"`
	Score       float64  `json:"score"`
	Members     int      `json:"members"`
	Favourites  int      `json:"favourites"`
	Description string   `json:"description"`
	Cover       string   `json:"cover"`
	Trailer     string   `json:"trailer"`
	Genres      []string `json:"genres"`
	Studios     []string `json:"studios"`
	Source      string   `json:"source"`
	URL         string   `json:"url"`
}

type malAnime struct {
	Data struct {
		MalID  int `json:"mal_id"`
		URL    string
		Images struct {
			JPG struct {
				LargeImageURL string `json:"large_image_url"`
			} `json:"jpg"`
			WebP struct {
				LargeImageURL string `json:"large_image_url"`
			} `json:"webp"`
		} `json:"images"`
		Trailer struct {
			URL string `json:"url"`
		} `json:"trailer"`
		Title         string   `json:"title"`
		TitleEnglish  string   `json:"title_english"`
		TitleJapanese string   `json:"title_japanese"`
		TitleSynonyms []string `json:"title_synonyms"`
		Type          string   `json:"type"`
		Source        string   `json:"source"`
		Episodes      int      `json:"episodes"`
		Status        string   `json:"status"`
		Duration      string   `json:"duration"`
		Score         float64  `json:"score"`
		Members       int      `json:"members"`
		Favorites     int      `json:"favorites"`
		Synopsis      string   `json:"synopsis"`
		Season        string   `json:"season"`
		Year          int      `json:"year"`
		Genres        []struct {
			Name string `json:"name"`
		} `json:"genres"`
		Studios []struct {
			Name string `json:"name"`
		} `json:"studios"`
	} `json:"data"`
}

func (c *Client) Anime(ctx context.Context, malID int) (AnimeDetail, error) {
	if malID <= 0 {
		return AnimeDetail{}, fmt.Errorf("metadata: invalid mal id %d", malID)
	}

	var res malAnime
	var err error
	for attempt := range 3 {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return AnimeDetail{}, ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		err = c.record(ctx, "tenrai-anime", TenraiAnimeURL, malID, func(body []byte) bool {
			res = malAnime{}
			return json.Unmarshal(body, &res) == nil && res.Data.MalID != 0
		})
		if err == nil || errors.Is(err, errNoRecord) {
			break
		}
	}
	if errors.Is(err, errNoRecord) {
		return AnimeDetail{}, fmt.Errorf("anime %d not found", malID)
	}
	if err != nil {
		return AnimeDetail{}, err
	}
	d := res.Data

	out := AnimeDetail{
		MalID:       d.MalID,
		Romaji:      d.Title,
		English:     d.TitleEnglish,
		Native:      d.TitleJapanese,
		Synonyms:    d.TitleSynonyms,
		Format:      d.Type,
		Status:      d.Status,
		Episodes:    d.Episodes,
		Duration:    d.Duration,
		Season:      d.Season,
		Year:        d.Year,
		Score:       d.Score,
		Members:     d.Members,
		Favourites:  d.Favorites,
		Description: d.Synopsis,
		Cover:       d.Images.WebP.LargeImageURL,
		Trailer:     d.Trailer.URL,
		Source:      d.Source,
		URL:         d.URL,
	}
	if out.Cover == "" {
		out.Cover = d.Images.JPG.LargeImageURL
	}
	for _, g := range d.Genres {
		out.Genres = append(out.Genres, g.Name)
	}
	for _, s := range d.Studios {
		out.Studios = append(out.Studios, s.Name)
	}
	return out, nil
}
