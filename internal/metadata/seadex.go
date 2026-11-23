package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const SeaDexURL = "https://seadex.moe/api/collections/entries/records"

type SeaDexRelease struct {
	AniListID    int
	InfoHash     string
	ReleaseGroup string
	Tracker      string
	URL          string
	IsBest       bool
	DualAudio    bool
	Incomplete   bool
	Notes        string
	Tags         []string
	Comparison   []string
	Files        []SeaDexFile
}

type SeaDexFile struct {
	Name   string `json:"name"`
	Length int64  `json:"length"`
}

type seadexPage struct {
	Page       int `json:"page"`
	PerPage    int `json:"perPage"`
	TotalItems int `json:"totalItems"`
	TotalPages int `json:"totalPages"`
	Items      []struct {
		AniListID  int    `json:"alID"`
		Incomplete bool   `json:"incomplete"`
		Notes      string `json:"notes"`
		Comparison string `json:"comparison"`
		Expand     struct {
			Torrents []struct {
				InfoHash     string       `json:"infoHash"`
				ReleaseGroup string       `json:"releaseGroup"`
				Tracker      string       `json:"tracker"`
				URL          string       `json:"url"`
				IsBest       bool         `json:"isBest"`
				DualAudio    bool         `json:"dualAudio"`
				Tags         any          `json:"tags"`
				Files        []SeaDexFile `json:"files"`
			} `json:"trs"`
		} `json:"expand"`
	} `json:"items"`
}

func (c *Client) SeaDex(ctx context.Context) ([]SeaDexRelease, error) {
	const perPage = 500

	var out []SeaDexRelease
	for page := 1; page <= 20; page++ {
		params := url.Values{
			"page":    {strconv.Itoa(page)},
			"perPage": {strconv.Itoa(perPage)},
			"expand":  {"trs"},
		}

		body, err := c.getStrict(ctx, c.url("seadex", SeaDexURL)+"?"+params.Encode())
		if err != nil {
			return out, err
		}

		var res seadexPage
		if err := json.Unmarshal(body, &res); err != nil {
			return out, fmt.Errorf("seadex: %w", err)
		}
		if len(res.Items) == 0 {
			break
		}

		for _, item := range res.Items {
			if item.AniListID == 0 {
				continue
			}
			for _, tr := range item.Expand.Torrents {
				hash := usableHash(tr.InfoHash)
				if hash == "" && tr.ReleaseGroup == "" {
					continue
				}
				out = append(out, SeaDexRelease{
					AniListID:    item.AniListID,
					InfoHash:     hash,
					ReleaseGroup: strings.TrimSpace(tr.ReleaseGroup),
					Tracker:      tr.Tracker,
					URL:          tr.URL,
					IsBest:       tr.IsBest,
					DualAudio:    tr.DualAudio,
					Incomplete:   item.Incomplete,
					Notes:        strings.TrimSpace(item.Notes),
					Tags:         toTags(tr.Tags),
					Comparison:   links(item.Comparison),
					Files:        tr.Files,
				})
			}
		}

		if res.TotalPages > 0 && page >= res.TotalPages {
			break
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("seadex returned no releases")
	}
	return out, nil
}

func usableHash(raw string) string {
	h := strings.ToLower(strings.TrimSpace(raw))
	if len(h) != 40 {
		return ""
	}
	for _, r := range h {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return ""
		}
	}
	return h
}

func links(field string) []string {
	var out []string
	for part := range strings.SplitSeq(field, ",") {
		if u, err := url.Parse(strings.TrimSpace(part)); err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" {
			out = append(out, u.String())
		}
	}
	return out
}

func toTags(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		var out []string
		for _, s := range strings.Split(t, ",") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
