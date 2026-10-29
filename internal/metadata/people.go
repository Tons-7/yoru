package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

const (
	TenraiPersonURL    = "https://api.tenrai.org/v1/people/%d/full"
	JikanPersonURL     = "https://api.jikan.moe/v4/people/%d/full"
	TenraiCharacterURL = "https://api.tenrai.org/v1/characters/%d/full"
	JikanCharacterURL  = "https://api.jikan.moe/v4/characters/%d/full"
)

// Shown is a show as MyAnimeList names it: its id there, title and poster.
type Shown struct {
	MalID int    `json:"malId"`
	Title string `json:"title"`
	Image string `json:"image,omitempty"`
}

// Role is one part a voice actor played: the character, in which show, and how big a part.
type Role struct {
	Role      string `json:"role,omitempty"`
	Show      Shown  `json:"show"`
	Character Person `json:"character"`
}

// Work is one show a person worked on off-mic, and as what.
type Work struct {
	Position string `json:"position"`
	Show     Shown  `json:"show"`
}

// Person is a voice actor or staff member, or a character in a role: as much as each page needs.
type Person struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Native    string `json:"native,omitempty"`
	Image     string `json:"image,omitempty"`
	About     string `json:"about,omitempty"`
	Birthday  string `json:"birthday,omitempty"`
	Favorites int    `json:"favorites,omitempty"`
	Roles     []Role `json:"roles,omitempty"`
	Work      []Work `json:"work,omitempty"`
}

// Appearance is a show a character is in, and how big a part.
type Appearance struct {
	Role string `json:"role,omitempty"`
	Show Shown  `json:"show"`
}

// CharacterPage is a character with the shows they're in and who voiced them.
type CharacterPage struct {
	ID          int          `json:"id"`
	Name        string       `json:"name"`
	Native      string       `json:"native,omitempty"`
	Image       string       `json:"image,omitempty"`
	About       string       `json:"about,omitempty"`
	Favorites   int          `json:"favorites,omitempty"`
	Appearances []Appearance `json:"appearances"`
	Voices      []Voice      `json:"voices"`
}

type images struct {
	JPG struct {
		ImageURL string `json:"image_url"`
	} `json:"jpg"`
}

type shownJSON struct {
	MalID  int    `json:"mal_id"`
	Title  string `json:"title"`
	Images images `json:"images"`
}

func (s shownJSON) shown() Shown {
	return Shown{MalID: s.MalID, Title: s.Title, Image: s.Images.JPG.ImageURL}
}

// ErrNoPerson: neither source has this person or character.
var ErrNoPerson = errors.New("metadata: no such person")

// PersonPage reads a voice actor or staff member with every part and credit, from Tenrai, else Jikan.
func (c *Client) PersonPage(ctx context.Context, malID int) (Person, error) {
	var res struct {
		Data struct {
			MalID      int    `json:"mal_id"`
			Name       string `json:"name"`
			GivenName  string `json:"given_name"`
			FamilyName string `json:"family_name"`
			Images     images `json:"images"`
			About      string `json:"about"`
			Birthday   string `json:"birthday"`
			Favorites  int    `json:"favorites"`
			Voices     []struct {
				Role      string    `json:"role"`
				Anime     shownJSON `json:"anime"`
				Character struct {
					MalID  int    `json:"mal_id"`
					Name   string `json:"name"`
					Images images `json:"images"`
				} `json:"character"`
			} `json:"voices"`
			Anime []struct {
				Position string    `json:"position"`
				Anime    shownJSON `json:"anime"`
			} `json:"anime"`
		} `json:"data"`
	}
	_, err := c.fromHosts(ctx, []host{{"tenrai-person", TenraiPersonURL}, {"jikan-person", JikanPersonURL}}, malID,
		func(body []byte) bool { return json.Unmarshal(body, &res) == nil && res.Data.MalID != 0 })
	if errors.Is(err, errNoRecord) {
		return Person{}, ErrNoPerson
	}
	if err != nil {
		return Person{}, err
	}
	d := res.Data
	out := Person{
		ID: d.MalID, Name: d.Name, Image: d.Images.JPG.ImageURL, About: strings.TrimSpace(d.About), Favorites: d.Favorites,
		// Family name first, as it's written.
		Native: strings.TrimSpace(d.FamilyName + " " + d.GivenName),
	}
	if len(d.Birthday) >= 10 {
		out.Birthday = d.Birthday[:10]
	}
	for _, v := range d.Voices {
		out.Roles = append(out.Roles, Role{Role: v.Role, Show: v.Anime.shown(),
			Character: Person{ID: v.Character.MalID, Name: v.Character.Name, Image: v.Character.Images.JPG.ImageURL}})
	}
	for _, w := range d.Anime {
		out.Work = append(out.Work, Work{Position: w.Position, Show: w.Anime.shown()})
	}
	return out, nil
}

// CharacterDetail reads a character with the shows they're in and their voices, from Tenrai, else Jikan.
func (c *Client) CharacterDetail(ctx context.Context, malID int) (CharacterPage, error) {
	var res struct {
		Data struct {
			MalID     int    `json:"mal_id"`
			Name      string `json:"name"`
			NameKanji string `json:"name_kanji"`
			Images    images `json:"images"`
			About     string `json:"about"`
			Favorites int    `json:"favorites"`
			Anime     []struct {
				Role  string    `json:"role"`
				Anime shownJSON `json:"anime"`
			} `json:"anime"`
			Voices []struct {
				Language string `json:"language"`
				Person   struct {
					MalID  int    `json:"mal_id"`
					Name   string `json:"name"`
					Images images `json:"images"`
				} `json:"person"`
			} `json:"voices"`
		} `json:"data"`
	}
	_, err := c.fromHosts(ctx, []host{{"tenrai-character", TenraiCharacterURL}, {"jikan-character", JikanCharacterURL}}, malID,
		func(body []byte) bool { return json.Unmarshal(body, &res) == nil && res.Data.MalID != 0 })
	if errors.Is(err, errNoRecord) {
		return CharacterPage{}, ErrNoPerson
	}
	if err != nil {
		return CharacterPage{}, err
	}
	d := res.Data
	out := CharacterPage{
		ID: d.MalID, Name: d.Name, Native: d.NameKanji, Image: d.Images.JPG.ImageURL, About: strings.TrimSpace(d.About),
		Favorites: d.Favorites, Appearances: []Appearance{}, Voices: []Voice{},
	}
	for _, a := range d.Anime {
		out.Appearances = append(out.Appearances, Appearance{Role: a.Role, Show: a.Anime.shown()})
	}
	// Japanese first, as the cast rail has it, then English; the rest after.
	for _, want := range []string{"Japanese", "English", ""} {
		for _, v := range d.Voices {
			if (want == "" && v.Language != "Japanese" && v.Language != "English") || v.Language == want {
				out.Voices = append(out.Voices, Voice{ID: v.Person.MalID, Name: v.Person.Name,
					Image: v.Person.Images.JPG.ImageURL, Language: v.Language})
			}
		}
	}
	return out, nil
}
