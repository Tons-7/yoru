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
	body, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > int64(limit) {
		return nil, fmt.Errorf("Jimaku sent more than %d MB", int64(limit)>>20)
	}
	return body, nil
}

func (c *Client) getJSON(ctx context.Context, key, target string, out any) error {
	body, err := c.get(ctx, key, target, 4<<20)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, out)
}

func (c *Client) Check(ctx context.Context, key string) error {
	var entries []json.RawMessage
	return c.getJSON(ctx, key, c.base+"/api/entries/search?anime=true&anilist_id=1", &entries)
}

func (c *Client) Files(ctx context.Context, key string, anilistID, episode int) ([]File, error) {
	var entries []struct {
		ID int `json:"id"`
	}
	if err := c.getJSON(ctx, key, fmt.Sprintf("%s/api/entries/search?anime=true&anilist_id=%d", c.base, anilistID), &entries); err != nil {
		return nil, err
	}
	out := []File{}
	var failed error
	for _, e := range entries {
		var files []File
		if err := c.getJSON(ctx, key, fmt.Sprintf("%s/api/entries/%d/files?episode=%d", c.base, e.ID, episode), &files); err != nil {
			if ctx.Err() != nil {
				return nil, err
			}
			failed = err
			continue
		}
		for _, f := range files {
			switch strings.ToLower(path.Ext(f.Name)) {
			case ".ass", ".ssa", ".srt", ".vtt":
				out = append(out, f)
			}
		}
	}
	if len(out) == 0 && failed != nil {
		return nil, failed
	}
	return out, nil
}

func (c *Client) Fetch(ctx context.Context, key, fileURL string) ([]byte, error) {
	u, err := url.Parse(fileURL)
	base, _ := url.Parse(c.base)
	if err != nil || u.Host != base.Host || u.Scheme != base.Scheme {
		return nil, fmt.Errorf("not a Jimaku file: %s", fileURL)
	}
	return c.get(ctx, key, fileURL, 16<<20)
}
