package server

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"yoru/internal/bt/metainfo"
	"yoru/internal/indexer"
	"yoru/internal/jobs"
	"yoru/internal/library"
	"yoru/internal/match"
	"yoru/internal/parse"
	"yoru/internal/score"
)

type indexHolder struct {
	mu sync.RWMutex
	ix *match.Index
}

func (h *indexHolder) get() *match.Index {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.ix
}

func (h *indexHolder) set(ix *match.Index) {
	h.mu.Lock()
	h.ix = ix
	h.mu.Unlock()
}

func (s *Server) corpusStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.store.CorpusStats(r.Context())
	if err != nil {
		s.fail(w, "corpus stats", err)
		return
	}

	indexed := 0
	if ix := s.index.get(); ix != nil {
		indexed = ix.Len()
	}
	send(w, http.StatusOK, map[string]any{
		"anime": stats.Anime, "titles": stats.Titles,
		"dead": stats.Dead, "indexed": indexed,
	})
}

func (s *Server) corpusRefresh(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("force") == "true"

	refresh := func(parent context.Context) error {
		ctx, cancel := context.WithTimeout(parent, 2*time.Hour)
		defer cancel()

		report, err := s.ingester.Run(ctx, force)
		if err != nil && report.Seeded == 0 && report.Fetched == 0 {
			return err
		}
		s.log.Info("corpus refreshed", "seeded", report.Seeded,
			"discovered", report.Discovered, "titles", report.Titles)
		return errors.Join(err, s.RebuildIndex(ctx))
	}

	var err error
	if s.jobs != nil {
		err = s.jobs.RunAs("corpus", refresh)
	} else if !s.refreshing.CompareAndSwap(false, true) {
		err = jobs.ErrRunning
	} else {
		go func() {
			defer s.refreshing.Store(false)
			if err := refresh(context.Background()); err != nil {
				s.log.Error("corpus refresh", "err", err)
			}
		}()
	}
	switch {
	case errors.Is(err, jobs.ErrRunning):
		send(w, http.StatusConflict, map[string]any{"error": "a refresh is already running"})
	case err != nil:
		s.fail(w, "corpus refresh", err)
	default:
		send(w, http.StatusAccepted, map[string]any{"status": "started"})
	}
}

func (s *Server) episodes(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.URL.Query().Get("id"))
	if id == 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "id is required"})
		return
	}

	titlesPending := false
	if s.enricher != nil {
		if _, err := s.enricher.Episodes(r.Context(), id); err != nil {
			s.log.Warn("fetch episodes", "anime", id, "err", err)
		}
		titlesPending = s.enricher.TitlesPending(r.Context(), id)
	}

	eps, err := s.store.Episodes(r.Context(), id)
	if err != nil {
		s.fail(w, "episodes", err)
		return
	}

	if len(eps) == 0 && s.importer != nil {
		if _, err := s.importer.Hydrate(r.Context(), []int{id}); err != nil {
			s.log.Warn("hydrate anime", "anime", id, "err", err)
		} else if eps, err = s.store.Episodes(r.Context(), id); err != nil {
			s.fail(w, "episodes", err)
			return
		}
	}

	if len(eps) > 0 && (eps[0].Planned || !s.store.Finished(r.Context(), id) && s.store.EpisodesFromReleasesOnly(r.Context(), id)) {
		s.deriveEpisodesAsync(id)
	}

	send(w, http.StatusOK, map[string]any{"items": eps, "count": len(eps), "titlesPending": titlesPending})
}

func (s *Server) deriveEpisodesAsync(animeID int) {
	if s.finder == nil || s.store.EpisodesDerived(context.Background(), animeID) {
		return
	}

	s.derivingMu.Lock()
	if _, busy := s.deriving[animeID]; busy {
		s.derivingMu.Unlock()
		return
	}
	s.deriving[animeID] = struct{}{}
	s.derivingMu.Unlock()

	go func() {
		defer func() {
			s.derivingMu.Lock()
			delete(s.deriving, animeID)
			s.derivingMu.Unlock()
		}()

		ctx, cancel := detached(5 * time.Minute)
		defer cancel()

		numbers, err := s.finder.EpisodeNumbers(ctx, animeID)
		if err != nil {
			s.log.Warn("derive episodes", "anime", animeID, "err", err)
			return
		}
		saved, err := s.store.SaveDerivedEpisodes(ctx, animeID, numbers)
		if err != nil {
			s.log.Warn("save derived episodes", "anime", animeID, "err", err)
			return
		}
		if saved > 0 {
			s.log.Info("episodes derived from releases", "anime", animeID, "count", saved)
		}
	}()
}

func (s *Server) cleanOrphans(w http.ResponseWriter, r *http.Request) {
	if s.cache == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "torrent engine unavailable"})
		return
	}

	var body struct {
		Names []string `json:"names"`
	}
	var files []library.Orphan
	var err error
	switch {
	case r.Method == http.MethodGet:
		files, err = s.cache.Orphans(r.Context(), true)
	case r.ContentLength != 0:
		if !decode(w, r, &body) {
			return
		}
		files, err = s.cache.RemoveOrphans(r.Context(), body.Names)
	default:
		send(w, http.StatusBadRequest, map[string]any{"error": "names to delete are required"})
		return
	}
	if err != nil {
		s.fail(w, "clean orphans", err)
		return
	}
	var bytes int64
	removed := map[string]bool{}
	for _, f := range files {
		bytes += f.Bytes
		removed[f.Name] = true
	}
	kept := []string{}
	for _, n := range body.Names {
		if !removed[n] {
			kept = append(kept, n)
		}
	}
	send(w, http.StatusOK, map[string]any{"files": files, "removed": len(files), "freedBytes": bytes, "kept": kept})
}

func (s *Server) refreshFillers(w http.ResponseWriter, r *http.Request) {
	if s.enricher == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "enricher unavailable"})
		return
	}
	n, err := s.enricher.Fillers(r.Context(), r.URL.Query().Get("force") == "true")
	if err != nil {
		s.fail(w, "filler refresh", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"episodes": n})
}

func (s *Server) refreshSeaDex(w http.ResponseWriter, r *http.Request) {
	if s.enricher == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "enricher unavailable"})
		return
	}
	n, err := s.enricher.SeaDex(r.Context(), r.URL.Query().Get("force") == "true")
	if err != nil {
		s.fail(w, "seadex refresh", err)
		return
	}
	count, _ := s.store.SeaDexCount(r.Context())
	send(w, http.StatusOK, map[string]any{"fetched": n, "stored": count})
}

func (s *Server) franchise(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.URL.Query().Get("id"))
	if id == 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "id is required"})
		return
	}

	if s.relations != nil {
		if err := s.relations.Ensure(r.Context(), id); err != nil {
			s.log.Warn("fetch relations", "anime", id, "err", err)
		}
	}

	fr, err := s.store.Franchise(r.Context(), id)
	if err != nil {
		s.fail(w, "franchise", err)
		return
	}

	var missing []int
	for _, season := range fr.Seasons {
		if season.Romaji == "" {
			missing = append(missing, season.ID)
		}
	}
	if len(missing) > 0 && s.importer != nil {
		if _, err := s.importer.Hydrate(r.Context(), missing); err != nil {
			s.log.Warn("hydrate franchise", "anime", id, "err", err)
		} else if fr, err = s.store.Franchise(r.Context(), id); err != nil {
			s.fail(w, "franchise", err)
			return
		}
	}

	related, err := s.store.Related(r.Context(), id)
	if err != nil {
		s.log.Warn("related entries", "anime", id, "err", err)
	}
	var blank []int
	for _, e := range related {
		if e.Romaji == "" {
			blank = append(blank, e.ID)
		}
	}
	if len(blank) > 0 && s.importer != nil {
		if _, err := s.importer.Hydrate(r.Context(), blank); err != nil {
			s.log.Warn("hydrate related", "anime", id, "err", err)
		} else if related, err = s.store.Related(r.Context(), id); err != nil {
			s.fail(w, "related entries", err)
			return
		}
	}

	send(w, http.StatusOK, map[string]any{
		"rootId":  fr.RootID,
		"seasons": fr.Seasons,
		"related": related,
	})
}

func (s *Server) episodeSources(w http.ResponseWriter, r *http.Request) {
	if s.finder == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{
			"error": "release search unavailable",
		})
		return
	}

	id, _ := strconv.Atoi(r.URL.Query().Get("id"))
	episode, _ := strconv.Atoi(r.URL.Query().Get("episode"))
	if id == 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "id is required"})
		return
	}

	if id > 0 {
		s.hydrate(r.Context(), []int{id})
	}

	season, _ := strconv.Atoi(r.URL.Query().Get("season"))
	prefs := s.preferences(r.Context(), id)
	switch a := r.URL.Query().Get("audio"); a {
	case "sub", "dub", "either":
		prefs.Audio = a
	}

	if m := strings.TrimSpace(r.URL.Query().Get("magnet")); m != "" && r.Method == http.MethodPost {
		magnet, err := metainfo.ParseMagnetUri(m)
		if err != nil {
			send(w, http.StatusBadRequest, map[string]any{"error": "not a magnet link: " + err.Error()})
			return
		}
		s.finder.Ask(id, episode, "", &indexer.Torrent{
			Title:    cmp.Or(magnet.DisplayName, "Pasted magnet "+magnet.InfoHash.HexString()[:8]),
			InfoHash: magnet.InfoHash.HexString(),
			Trackers: magnet.Trackers,
			Indexer:  library.Pasted,
		})
	}
	if q := r.URL.Query().Get("q"); q != "" && r.Method == http.MethodPost {
		s.finder.Ask(id, episode, q, nil)
	}

	got, err := s.finder.Find(r.Context(), library.Request{
		AnimeID: id,
		Episode: episode,
		Season:  season,
		Prefs:   prefs,
	})
	if err != nil {
		s.fail(w, "episode sources", err)
		return
	}
	send(w, http.StatusOK, got)
}

func (s *Server) preferences(ctx context.Context, animeID int) score.Preferences {
	resolved, err := s.store.Prefs(ctx, animeID)
	if err != nil {
		s.log.Warn("read preferences", "anime", animeID, "err", err)
		return score.DefaultPreferences()
	}
	return library.Preferences(resolved)
}

func (s *Server) RebuildIndex(ctx context.Context) error {
	ix, err := s.store.BuildIndex(ctx)
	if err != nil {
		return err
	}
	s.index.set(ix)
	s.log.Info("match index built", "anime", ix.Len())
	return nil
}

func (s *Server) match(w http.ResponseWriter, r *http.Request) {
	title := r.URL.Query().Get("title")
	if title == "" {
		send(w, http.StatusBadRequest, map[string]any{"error": "title is required"})
		return
	}

	ix := s.index.get()
	if ix == nil || ix.Len() == 0 {
		send(w, http.StatusPreconditionFailed, map[string]any{
			"error": "title corpus is empty; POST /api/corpus/refresh first",
		})
		return
	}

	rel := parse.Parse(title)
	q := match.Query{
		Title:   rel.Title,
		Season:  rel.Season,
		Episode: rel.Episode,
	}
	if year, _ := strconv.Atoi(r.URL.Query().Get("year")); year > 0 {
		q.Year = year
	}
	if boosts, err := s.store.MatchBoosts(r.Context()); err == nil {
		q.Boost = boosts
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 5
	}

	best, ok := ix.Match(q)
	send(w, http.StatusOK, map[string]any{
		"parsed":     rel,
		"query":      q,
		"matched":    ok,
		"best":       best,
		"candidates": ix.Rank(q, limit),
	})
}
