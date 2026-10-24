package metadata

import (
	"context"
	"encoding/json"
	"fmt"
)

// TrashURL is TRaSH Guides' custom formats for Sonarr (MIT); the anime ones grade release groups.
const TrashURL = "https://raw.githubusercontent.com/TRaSH-Guides/Guides/master/docs/json/sonarr/cf/"

// GroupTier is one group on one of TRaSH's anime lists: a BD or WEB tier (1 is best), or the low-quality list.
type GroupTier struct {
	Kind    string // "bd", "web" or "lq"
	Level   int
	Name    string
	Pattern string // TRaSH's regex for the group in a release title
}

// trashLists are the files read, by kind and how many tiers each has.
var trashLists = []struct {
	kind, prefix string
	levels       int
}{
	{"bd", "anime-bd-tier-%02d", 8},
	{"web", "anime-web-tier-%02d", 6},
	{"lq", "anime-lq-groups", 0},
}

type trashFormat struct {
	Specifications []struct {
		Name           string `json:"name"`
		Implementation string `json:"implementation"`
		Negate         bool   `json:"negate"`
		Fields         struct {
			Value any `json:"value"`
		} `json:"fields"`
	} `json:"specifications"`
}

// GroupTiers reads every anime tier list; one failing fails the lot, so a half list never replaces a whole one.
func (c *Client) GroupTiers(ctx context.Context) ([]GroupTier, error) {
	var out []GroupTier
	for _, list := range trashLists {
		for level := range max(list.levels, 1) {
			name := list.prefix
			if list.levels > 0 {
				name = fmt.Sprintf(list.prefix, level+1)
			}
			body, err := c.getStrict(ctx, c.url("trash", TrashURL)+name+".json")
			if err != nil {
				return nil, fmt.Errorf("trash %s: %w", name, err)
			}
			var f trashFormat
			if err := json.Unmarshal(body, &f); err != nil {
				return nil, fmt.Errorf("trash %s: %w", name, err)
			}
			for _, s := range f.Specifications {
				pattern, ok := s.Fields.Value.(string)
				if s.Implementation != "ReleaseTitleSpecification" || s.Negate || !ok || pattern == "" {
					continue
				}
				out = append(out, GroupTier{Kind: list.kind, Level: level + 1, Name: s.Name, Pattern: pattern})
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("trash lists named no groups")
	}
	return out, nil
}
