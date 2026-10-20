package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"yoru/internal/config"
	"yoru/internal/indexer"
	"yoru/internal/store"
)

// sitesKey holds the release sites chosen in the app, as JSON. Once saved, that list replaces config.toml's
// [[indexer]] blocks, which are read until then.
const sitesKey = "indexer.sites"

// maxSites bounds a saved list: every search asks each site, several queries an episode.
const maxSites = 12

// LoadSites is the list in use: the one saved in the app, or config.toml's blocks; saved says which.
func LoadSites(ctx context.Context, st *store.Store, cfg config.Config) (sites []indexer.Site, saved bool, err error) {
	raw, err := st.Setting(ctx, sitesKey)
	if err == nil && raw != "" {
		if err = json.Unmarshal([]byte(raw), &sites); err == nil {
			return sites, true, nil
		}
		sites = nil
	}
	// Unreadable, the config's list stands in: an empty one would leave nothing to search.
	for _, in := range cfg.Indexers {
		sites = append(sites, indexer.Site{Type: in.Type, URL: in.URL, Adult: in.Adult})
	}
	return sites, false, err
}

type siteView struct {
	indexer.Site
	// Problem: why it isn't searched (a mistyped address, adult on a feed that has none).
	Problem string `json:"problem,omitempty"`
}

func (s *Server) sitesBody(ctx context.Context) (map[string]any, error) {
	sites, saved, err := LoadSites(ctx, s.store, s.cfg)
	if err != nil {
		return nil, err
	}
	out := make([]siteView, 0, len(sites))
	for _, site := range sites {
		v := siteView{Site: site}
		if _, err := indexer.Build(site.Type, site.URL, site.Adult); err != nil {
			v.Problem = err.Error()
		}
		out = append(out, v)
	}
	return map[string]any{
		"sites": out, "saved": saved, "configPath": s.cfg.ConfigPath(),
		"kinds": []string{indexer.KindNyaa, indexer.KindTokyoTosho},
	}, nil
}

func (s *Server) sitesList(w http.ResponseWriter, r *http.Request) {
	body, err := s.sitesBody(r.Context())
	if err != nil {
		s.fail(w, "release sites", err)
		return
	}
	send(w, http.StatusOK, body)
}

// cleanSites trims and checks a list sent from the app; every site must build, and none twice.
func cleanSites(in []indexer.Site) ([]indexer.Site, error) {
	if len(in) > maxSites {
		return nil, fmt.Errorf("at most %d sites", maxSites)
	}
	seen := map[string]bool{}
	out := make([]indexer.Site, 0, len(in))
	for _, site := range in {
		site.Type = strings.ToLower(strings.TrimSpace(site.Type))
		site.URL = strings.TrimRight(strings.TrimSpace(site.URL), "/")
		if _, err := indexer.Build(site.Type, site.URL, site.Adult); err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%s %s %v", site.Type, strings.ToLower(site.URL), site.Adult)
		if seen[key] {
			return nil, fmt.Errorf("%s is listed twice", site.URL)
		}
		seen[key] = true
		out = append(out, site)
	}
	return out, nil
}

// saveSites replaces the list and searches with it at once: no restart.
func (s *Server) saveSites(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Sites []indexer.Site `json:"sites"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		send(w, http.StatusBadRequest, map[string]any{"error": "bad request"})
		return
	}
	sites, err := cleanSites(body.Sites)
	if err != nil {
		send(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	raw, _ := json.Marshal(sites)
	if err := s.store.SetSetting(r.Context(), sitesKey, string(raw)); err != nil {
		s.fail(w, "save release sites", err)
		return
	}
	if s.sites != nil {
		s.sites.Set(sites)
	}
	s.log.Info("release sites saved", "sites", len(sites))
	s.sitesList(w, r)
}

// How long a test search may take; a site slower than this would make every play wait as long.
const siteTestTimeout = 20 * time.Second

// testSite runs one search on a site before it is saved, so a wrong address shows here, not at the next play.
func (s *Server) testSite(w http.ResponseWriter, r *http.Request) {
	var site indexer.Site
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&site); err != nil {
		send(w, http.StatusBadRequest, map[string]any{"error": "bad request"})
		return
	}
	cleaned, err := cleanSites([]indexer.Site{site})
	if err != nil {
		send(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	src, _ := indexer.Build(cleaned[0].Type, cleaned[0].URL, cleaned[0].Adult)
	ctx, cancel := context.WithTimeout(r.Context(), siteTestTimeout)
	defer cancel()
	start := time.Now()
	// A word nearly every anime release carries, so a working site always answers with something.
	results, err := src.Search(ctx, indexer.Query{Text: "1080p"})
	took := time.Since(start)
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		send(w, http.StatusBadGateway, map[string]any{"error": fmt.Sprintf("no answer within %d seconds", int(siteTestTimeout.Seconds()))})
	case err != nil:
		send(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
	case len(results) == 0:
		send(w, http.StatusBadGateway, map[string]any{"error": "the site answered with no results; check the address and type"})
	default:
		send(w, http.StatusOK, map[string]any{"count": len(results), "ms": took.Milliseconds()})
	}
}
