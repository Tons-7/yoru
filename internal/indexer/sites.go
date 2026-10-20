package indexer

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
)

// Site is one search site as configured: its feed format, address, and whether it is the adult one.
type Site struct {
	Type  string `json:"type"`
	URL   string `json:"url"`
	Adult bool   `json:"adult"`
}

func (s Site) key() string {
	k := strings.ToLower(s.Type) + " " + s.URL
	if s.Adult {
		k += " adult"
	}
	return k
}

// ErrNoSites: nothing to search with. A Sites view stays non-nil, so callers check Configured instead.
var ErrNoSites = errors.New("no release sites configured")

// Sites holds the configured sites, swapped whole when they change; a search in flight finishes on the old set.
type Sites struct {
	mu  sync.Mutex // serialises Set, which reuses the previous set's caches
	cur atomic.Pointer[siteSet]
}

type siteSet struct {
	general, adult []Source
	cached         map[string]Source
}

func NewSites() *Sites {
	s := &Sites{}
	s.cur.Store(&siteSet{})
	return s
}

// Set builds the list in order, each site cached; one that won't build is skipped and its error returned.
// A site kept from the previous list keeps its cache, so editing one doesn't empty the others'.
func (s *Sites) Set(list []Site) []error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.cur.Load()
	next := &siteSet{cached: map[string]Source{}}
	var errs []error
	for _, site := range list {
		src, ok := prev.cached[site.key()]
		if !ok {
			built, err := Build(site.Type, site.URL, site.Adult)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			src = NewCached(built)
		}
		next.cached[site.key()] = src
		if site.Adult {
			next.adult = append(next.adult, src)
		} else {
			next.general = append(next.general, src)
		}
	}
	s.cur.Store(next)
	return errs
}

// Counts are the sites in use now, general and adult.
func (s *Sites) Counts() (general, adult int) {
	cur := s.cur.Load()
	return len(cur.general), len(cur.adult)
}

// General searches the sites for every show; Adult those for titles the catalogue marks adult.
func (s *Sites) General() Source { return view{s, false} }
func (s *Sites) Adult() Source   { return view{s, true} }

type view struct {
	sites *Sites
	adult bool
}

func (v view) sources() []Source {
	cur := v.sites.cur.Load()
	if v.adult {
		return cur.adult
	}
	return cur.general
}

func (v view) Name() string { return "multi" }

func (v view) Empty() bool { return len(v.sources()) == 0 }

func (v view) Search(ctx context.Context, q Query) ([]Torrent, error) {
	srcs := v.sources()
	if len(srcs) == 0 {
		return nil, ErrNoSites
	}
	return Multi{Sources: srcs}.Search(ctx, q)
}

// Configured reports a source with a site behind it: nil, or a Sites view with none, has nothing to search.
func Configured(src Source) bool {
	if src == nil {
		return false
	}
	if e, ok := src.(interface{ Empty() bool }); ok {
		return !e.Empty()
	}
	return true
}
