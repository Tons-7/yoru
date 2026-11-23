package indexer

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
)

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

var ErrNoSites = errors.New("no release sites configured")

type Sites struct {
	mu  sync.Mutex
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

func (s *Sites) Set(list []Site) []error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.cur.Load()
	next := &siteSet{cached: map[string]Source{}}
	var errs []error
	for _, site := range list {
		if _, dup := next.cached[site.key()]; dup {
			continue
		}
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

func (s *Sites) Counts() (general, adult int) {
	cur := s.cur.Load()
	return len(cur.general), len(cur.adult)
}

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

func Configured(src Source) bool {
	if src == nil {
		return false
	}
	if e, ok := src.(interface{ Empty() bool }); ok {
		return !e.Empty()
	}
	return true
}
