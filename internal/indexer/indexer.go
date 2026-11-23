package indexer

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

type Torrent struct {
	Title        string    `json:"title"`
	InfoHash     string    `json:"infoHash"`
	Size         int64     `json:"size"`
	Seeders      int       `json:"seeders"`
	Leechers     int       `json:"leechers"`
	SeedersKnown bool      `json:"seedersKnown"`
	Downloads    int       `json:"downloads"`
	Category     string    `json:"category"`
	Trusted      bool      `json:"trusted"`
	Remake       bool      `json:"remake"`
	Published    time.Time `json:"published"`
	Link         string    `json:"link"`
	Indexer      string    `json:"indexer"`
	Trackers     []string  `json:"trackers,omitempty"`
}

type Query struct {
	Text     string
	Category string
	Filter   Filter
	Uploader string
	Limit    int
}

type Filter int

const (
	FilterNone     Filter = 0
	FilterNoRemake Filter = 1
	FilterTrusted  Filter = 2
)

const (
	CategoryAnime          = "1_0"
	CategoryEnglishSubbed  = "1_2"
	CategoryNonEnglishSubs = "1_3"
	CategoryRaw            = "1_4"
)

type Source interface {
	Name() string
	Search(ctx context.Context, q Query) ([]Torrent, error)
}

var trackers = []string{
	"udp://open.stealth.si:80/announce",
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://exodus.desync.com:6969/announce",
	"udp://tracker.torrent.eu.org:451/announce",
}

const nyaaTracker = "http://nyaa.tracker.wf:7777/announce"

func (t Torrent) Magnet() string {
	if t.InfoHash == "" {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "magnet:?xt=urn:btih:%s", t.InfoHash)
	if t.Title != "" {
		fmt.Fprintf(&b, "&dn=%s", url.QueryEscape(t.Title))
	}
	var first []string
	if t.Indexer == "nyaa" {
		first = []string{nyaaTracker}
	}
	seen := map[string]bool{}
	for _, tr := range slices.Concat(first, t.Trackers, trackers) {
		if tr == "" || seen[tr] {
			continue
		}
		seen[tr] = true
		fmt.Fprintf(&b, "&tr=%s", url.QueryEscape(tr))
	}
	return b.String()
}

func trackersOf(magnet string) []string {
	_, query, ok := strings.Cut(magnet, "?")
	if !ok {
		return nil
	}
	values, _ := url.ParseQuery(query)
	return values["tr"]
}

type Multi struct {
	Sources []Source
}

func (m Multi) Name() string { return "multi" }

func (m Multi) Search(ctx context.Context, q Query) ([]Torrent, error) {
	type result struct {
		torrents []Torrent
		err      error
	}
	results := make([]result, len(m.Sources))

	var wg sync.WaitGroup
	for i, src := range m.Sources {
		wg.Go(func() {
			t, err := src.Search(ctx, q)
			if err != nil {
				err = fmt.Errorf("%s: %w", src.Name(), err)
			}
			results[i] = result{torrents: t, err: err}
		})
	}
	wg.Wait()

	seen := make(map[string]int)
	var out []Torrent
	var errs []error

	for _, r := range results {
		if r.err != nil {
			errs = append(errs, r.err)
			continue
		}
		for _, t := range r.torrents {
			key := strings.ToLower(t.InfoHash)
			if i, dup := seen[key]; dup {
				if t.SeedersKnown && (!out[i].SeedersKnown || t.Seeders > out[i].Seeders) {
					out[i].Seeders = t.Seeders
					out[i].Leechers = t.Leechers
					out[i].SeedersKnown = true
				}
				out[i].Trusted = out[i].Trusted || t.Trusted
				out[i].Remake = out[i].Remake || t.Remake
				if out[i].Category == "" {
					out[i].Category = t.Category
				}
				continue
			}
			seen[key] = len(out)
			out = append(out, t)
		}
	}

	if len(errs) == len(results) && len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return out, nil
}
