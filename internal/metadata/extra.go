package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

const (
	TenraiFullURL  = "https://api.tenrai.org/v1/anime/%d/full"
	TenraiStaffURL = "https://api.tenrai.org/v1/anime/%d/staff"
)

type Theme struct {
	Title    string `json:"title"`
	Artist   string `json:"artist,omitempty"`
	Episodes string `json:"episodes,omitempty"`
}

type Credit struct {
	ID    int    `json:"id,omitempty"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	Image string `json:"image,omitempty"`
}

type Extra struct {
	Openings     []Theme  `json:"openings,omitempty"`
	Endings      []Theme  `json:"endings,omitempty"`
	Staff        []Credit `json:"staff,omitempty"`
	Trailer      string   `json:"trailer,omitempty"`
	StaffMissing bool     `json:"-"`
}

type fullResponse struct {
	Data struct {
		MalID int `json:"mal_id"`
		Theme struct {
			Openings []string `json:"openings"`
			Endings  []string `json:"endings"`
		} `json:"theme"`
		Trailer struct {
			URL string `json:"url"`
		} `json:"trailer"`
	} `json:"data"`
}

type staffResponse struct {
	Data []struct {
		Person struct {
			MalID  int    `json:"mal_id"`
			Name   string `json:"name"`
			Images struct {
				JPG struct {
					ImageURL string `json:"image_url"`
				} `json:"jpg"`
			} `json:"images"`
		} `json:"person"`
		Positions []string `json:"positions"`
	} `json:"data"`
}

var keyRoles = []string{
	"Original Creator",
	"Director",
	"Series Composition",
	"Character Design",
	"Music",
	"Sound Director",
	"Art Director",
}

var roleScope = regexp.MustCompile(`\s*\([^)]*\)\s*$`)

var (
	themeIndex = regexp.MustCompile(`^S?\d+:\s*`)
	themeEps   = regexp.MustCompile(`\s*\(eps?\s+([^)]*)\)\s*$`)
)

func parseTheme(line string) Theme {
	rest := strings.TrimSpace(themeIndex.ReplaceAllString(strings.TrimSpace(line), ""))

	var out Theme
	if m := themeEps.FindStringSubmatch(rest); m != nil {
		out.Episodes = strings.TrimSpace(m[1])
		rest = themeEps.ReplaceAllString(rest, "")
	}

	if cut := strings.LastIndex(rest, `" by `); cut != -1 {
		out.Title = strings.Trim(rest[:cut], `"`)
		out.Artist = strings.TrimSpace(rest[cut+len(`" by `):])
		return out
	}
	if cut := strings.LastIndex(rest, " by "); cut != -1 {
		out.Title = strings.Trim(rest[:cut], `" `)
		out.Artist = strings.TrimSpace(rest[cut+len(" by "):])
		return out
	}

	out.Title = strings.Trim(rest, `"`)
	return out
}

func (c *Client) Extra(ctx context.Context, malID int) (Extra, error) {
	if malID == 0 {
		return Extra{}, nil
	}

	var full fullResponse
	err := c.record(ctx, "tenrai-full", TenraiFullURL, malID,
		func(body []byte) bool {
			full = fullResponse{}
			return json.Unmarshal(body, &full) == nil && full.Data.MalID != 0
		})
	if errors.Is(err, errNoRecord) {
		return Extra{}, nil
	}
	if err != nil {
		return Extra{}, err
	}

	out := Extra{Trailer: full.Data.Trailer.URL}
	for _, line := range full.Data.Theme.Openings {
		out.Openings = append(out.Openings, parseTheme(line))
	}
	for _, line := range full.Data.Theme.Endings {
		out.Endings = append(out.Endings, parseTheme(line))
	}

	var staff staffResponse
	err = c.record(ctx, "tenrai-staff", TenraiStaffURL, malID,
		func(body []byte) bool {
			staff = staffResponse{}
			return json.Unmarshal(body, &staff) == nil && len(staff.Data) > 0
		})
	if err != nil && !errors.Is(err, errNoRecord) {
		out.StaffMissing = true
		return out, nil
	}

	const perRole = 2
	seen := map[string]bool{}
	for _, role := range keyRoles {
		var kept int
		for _, entry := range staff.Data {
			if kept >= perRole {
				break
			}
			for _, position := range entry.Positions {
				if roleScope.ReplaceAllString(position, "") != role {
					continue
				}
				if key := role + "|" + entry.Person.Name; !seen[key] {
					seen[key] = true
					kept++
					out.Staff = append(out.Staff, Credit{
						ID:    entry.Person.MalID,
						Name:  entry.Person.Name,
						Role:  role,
						Image: entry.Person.Images.JPG.ImageURL,
					})
				}
				break
			}
		}
	}
	return out, nil
}
