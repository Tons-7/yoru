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

const sitesKey = "indexer.sites"

const maxSites = 12

func LoadSites(ctx context.Context, st *store.Store, cfg config.Config) (sites []indexer.Site, saved bool, err error) {
	raw, err := st.Setting(ctx, sitesKey)
	if err == nil && raw != "" {
		if err = json.Unmarshal([]byte(raw), &sites); err == nil {
			return sites, true, nil
		}
		sites = nil
	}
	for _, in := range cfg.Indexers {
		sites = append(sites, indexer.Site{Type: in.Type, URL: in.URL, Adult: in.Adult})
	}
	return sites, false, err
}

type siteView struct {
	indexer.Site
	Problem string `json:"problem,omitempty"`
}

func (s *Server) sitesBody(ctx context.Context) (map[string]any, error) {
	sites, saved, err := LoadSites(ctx, s.store, s.cfg)
	if err != nil {
		var syntax *json.SyntaxError
		var shape *json.UnmarshalTypeError
		if !errors.As(err, &syntax) && !errors.As(err, &shape) {
			return nil, err
		}
		s.log.Warn("saved release sites unreadable, showing the config list", "err", err)
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

const siteTestTimeout = 20 * time.Second

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
