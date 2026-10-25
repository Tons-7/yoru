// Package jimaku finds Japanese subtitles on jimaku.cc for an AniList entry's episode, with the viewer's key.
package jimaku

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const site = "https://jimaku.cc"

var (
	ErrKey  = errors.New("Jimaku turned the API key down; check it under Settings, Language & display")
	ErrBusy = errors.New("Jimaku asks to wait a moment before asking again")
)

// File is one subtitle file of an entry.
type File struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size int64  `json:"size"`
}

type Client struct {
	http *http.Client
	base string
}

func New() *Client { return &Client{http: &http.Client{Timeout: 20 * time.Second}, base: site} }

// NewAt is a client of another site in Jimaku's shape, for tests.
func NewAt(base string, c *http.Client) *Client { return &Client{http: c, base: base} }

func (c *Client) get(ctx context.Context, key, target string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", key)
	req.Header.Set("User-Agent", "Yoru")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Jimaku: %w", err)
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return nil, ErrKey
	case res.StatusCode == http.StatusTooManyRequests:
		return nil, ErrBusy
	case res.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("Jimaku answered %s", res.Status)
	}
	return io.ReadAll(io.LimitReader(res.Body, limit))
}

func (c *Client) getJSON(ctx context.Context, key, target string, out any) error {
	body, err := c.get(ctx, key, target, 4<<20)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

// Check asks something that needs the key, to say whether it works.
func (c *Client) Check(ctx context.Context, key string) error {
	var entries []json.RawMessage
	return c.getJSON(ctx, key, c.base+"/api/entries/search?anime=true&anilist_id=1", &entries)
}

// Files are an episode's subtitle files from every entry for the AniList id, archives (season packs) left out.
func (c *Client) Files(ctx context.Context, key string, anilistID, episode int) ([]File, error) {
	var entries []struct {
		ID int `json:"id"`
	}
	if err := c.getJSON(ctx, key, fmt.Sprintf("%s/api/entries/search?anime=true&anilist_id=%d", c.base, anilistID), &entries); err != nil {
		return nil, err
	}
	out := []File{}
	for _, e := range entries {
		var files []File
		if err := c.getJSON(ctx, key, fmt.Sprintf("%s/api/entries/%d/files?episode=%d", c.base, e.ID, episode), &files); err != nil {
			return nil, err
		}
		for _, f := range files {
			switch strings.ToLower(path.Ext(f.Name)) {
			case ".ass", ".ssa", ".srt", ".vtt":
				out = append(out, f)
			}
		}
	}
	return out, nil
}

// Fetch downloads a file Files listed; only from Jimaku itself, so a listing can't send Yoru elsewhere.
func (c *Client) Fetch(ctx context.Context, key, fileURL string) ([]byte, error) {
	u, err := url.Parse(fileURL)
	base, _ := url.Parse(c.base)
	if err != nil || u.Host != base.Host || u.Scheme != base.Scheme {
		return nil, fmt.Errorf("not a Jimaku file: %s", fileURL)
	}
	return c.get(ctx, key, fileURL, 16<<20)
}
