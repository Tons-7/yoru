package indexer

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const (
	tokyoDelay = 1200 * time.Millisecond

	tokyoAnimeCategory = "1"
)

type TokyoTosho struct {
	http     *http.Client
	base     string
	category string
	limiter  *rate.Limiter
}

type TokyoToshoOption func(*TokyoTosho)

func WithTokyoToshoDelay(d time.Duration) TokyoToshoOption {
	return func(t *TokyoTosho) { t.limiter = rate.NewLimiter(rate.Every(d), 1) }
}

func NewTokyoTosho(base string, opts ...TokyoToshoOption) *TokyoTosho {
	t := &TokyoTosho{
		http:     &http.Client{Timeout: 30 * time.Second},
		base:     strings.TrimRight(base, "/"),
		category: tokyoAnimeCategory,
		limiter:  rate.NewLimiter(rate.Every(tokyoDelay), 1),
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

func (t *TokyoTosho) Name() string { return "tokyotosho" }

type tokyoFeed struct {
	Items []tokyoItem `xml:"channel>item"`
}

type tokyoItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	PubDate     string `xml:"pubDate"`
	Category    string `xml:"category"`
	Description string `xml:"description"`
}

var (
	tokyoMagnet     = regexp.MustCompile(`href="(magnet:[^"]+)"`)
	tokyoSize       = regexp.MustCompile(`(?i)Size:\s*([0-9.,]+\s*[a-z]+)`)
	tokyoAuthorized = regexp.MustCompile(`(?i)Authorized:\s*(\w+)`)
)

func (t *TokyoTosho) Search(ctx context.Context, q Query) ([]Torrent, error) {
	if err := t.limiter.Wait(ctx); err != nil {
		return nil, err
	}

	params := url.Values{"filter": {t.category}}
	if q.Text != "" {
		params.Set("terms", q.Text)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		t.base+"/rss.php?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "yoru/0.1")
	req.Header.Set("Accept", "application/rss+xml")

	res, err := t.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, err
	}

	if !bytes.Contains(body, []byte("<item>")) {
		if bytes.Contains(body, []byte("</rss>")) || bytes.Contains(body, []byte("<channel")) {
			return nil, nil
		}
		return nil, errors.New("the answer is not a search feed")
	}

	var feed tokyoFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("parse feed: %w", err)
	}

	out := make([]Torrent, 0, len(feed.Items))
	for _, item := range feed.Items {
		torrent, ok := t.torrent(item)
		if !ok {
			continue
		}
		out = append(out, torrent)
		if q.Limit > 0 && len(out) >= q.Limit {
			break
		}
	}
	return out, nil
}

func (t *TokyoTosho) torrent(item tokyoItem) (Torrent, bool) {
	title := strings.TrimSpace(item.Title)
	if title == "" {
		return Torrent{}, false
	}

	magnet := tokyoMagnet.FindStringSubmatch(item.Description)
	if magnet == nil {
		return Torrent{}, false
	}
	link := html.UnescapeString(magnet[1])
	hash := infoHashFromMagnet(link)
	if hash == "" {
		return Torrent{}, false
	}

	out := Torrent{
		Title:     title,
		InfoHash:  hash,
		Category:  strings.TrimSpace(item.Category),
		Published: parseTime(item.PubDate),
		Link:      strings.TrimSpace(item.GUID),
		Indexer:   t.Name(),
		Trackers:  trackersOf(link),
	}
	if m := tokyoSize.FindStringSubmatch(item.Description); m != nil {
		out.Size = ParseSize(m[1])
	}
	if m := tokyoAuthorized.FindStringSubmatch(item.Description); m != nil {
		out.Trusted = strings.EqualFold(m[1], "yes")
	}
	return out, true
}
