package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"yoru/internal/anilist"
	"yoru/internal/library"
	"yoru/internal/metadata"
	"yoru/internal/store"
)

func (s *Server) random(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	year, _ := strconv.Atoi(q.Get("year"))
	format := q.Get("format")
	genres := csv(q.Get("genres"))

	if len(genres) == 0 {
		ids, err := s.store.RandomIDs(r.Context(), format, year, 5)
		if err != nil {
			s.fail(w, "random", err)
			return
		}
		for _, id := range ids {
			if body, ok, _ := s.animeCard(r, id); ok {
				send(w, http.StatusOK, body)
				return
			}
		}
	}

	m, err := s.anilist.Random(r.Context(), format, genres, year)
	if err != nil {
		s.fail(w, "random", err)
		return
	}
	if body, ok, _ := s.animeCard(r, m.ID); ok {
		send(w, http.StatusOK, body)
		return
	}
	send(w, http.StatusNotFound, map[string]any{"error": "no anime matched"})
}

func (s *Server) characters(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id == 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "bad anime id"})
		return
	}
	if s.enricher == nil {
		send(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}

	cast, err := s.enricher.Characters(r.Context(), id)
	if err != nil {
		s.log.Warn("characters", "anime", id, "err", err)
		send(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}
	send(w, http.StatusOK, map[string]any{"items": cast, "count": len(cast)})
}

func (s *Server) extra(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id == 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "bad anime id"})
		return
	}
	if s.enricher == nil {
		send(w, http.StatusOK, metadata.Extra{})
		return
	}

	out, err := s.enricher.Extra(r.Context(), id)
	if err != nil {
		s.log.Warn("anime extra", "anime", id, "err", err)
		send(w, http.StatusOK, metadata.Extra{})
		return
	}
	send(w, http.StatusOK, out)
}

func (s *Server) anime(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id == 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "bad anime id"})
		return
	}

	body, ok, down := s.animeCard(r, id)
	if down {
		send(w, http.StatusBadGateway, map[string]any{"error": "couldn't reach the catalogue; try again"})
		return
	}
	if !ok {
		send(w, http.StatusNotFound, map[string]any{"error": "anime not found"})
		return
	}

	if entry, err := s.store.ListEntry(r.Context(), id); err == nil && entry.AnimeID != 0 {
		body["listStatus"] = entry.Status
		body["progress"] = entry.Progress
		body["episodeCount"] = entry.Episodes
		body["repeat"] = entry.Repeat
		body["score"] = entry.Score
	}
	if b, err := s.store.Bookmark(r.Context(), id); err == nil {
		body["bookmark"] = b
	}
	if f, err := s.store.FollowOf(r.Context(), id); err == nil {
		body["notify"] = f
	}

	if p, ok, err := s.store.LastInProgress(r.Context(), id); err == nil && ok {
		if episode, err := strconv.Atoi(p.EpKey); err == nil {
			body["resume"] = map[string]any{
				"episode": episode, "position": p.Position, "duration": p.Duration,
			}
		}
	}
	send(w, http.StatusOK, body)
}

const malWait = 8 * time.Second

func (s *Server) animeCard(r *http.Request, id int) (body map[string]any, ok, down bool) {
	titles := s.store.TitleMode(r.Context(), 0)

	onList, err := s.store.ListProgress(r.Context())
	if err != nil {
		s.log.Warn("list progress", "err", err)
	}
	progress, listed := onList[id]

	if malID := store.MALIDOf(id); malID > 0 {
		wait, cancel := context.WithTimeout(r.Context(), malWait)
		detail, err := s.enricher.Anime(wait, malID)
		cancel()
		if err == nil {
			var english *string
			if detail.English != "" {
				english = &detail.English
			}
			return map[string]any{
				"id":       id,
				"malId":    malID,
				"source":   "myanimelist",
				"anime":    detail,
				"title":    store.PickTitle(titles, detail.Romaji, english),
				"onList":   listed,
				"progress": progress,
				"sync":     syncable(false, true),
			}, true, false
		}

		s.log.Warn("myanimelist lookup failed, using corpus record", "mal", malID, "err", err)

		rec, recErr := s.store.CorpusRecord(r.Context(), id)
		if recErr != nil || rec.AnimeID == 0 {
			return nil, false, true
		}
		return map[string]any{
			"id":       id,
			"malId":    malID,
			"source":   "corpus",
			"anime":    rec,
			"title":    rec.Primary,
			"partial":  true,
			"onList":   listed,
			"progress": progress,
			"sync":     syncable(false, true),
		}, true, false
	}

	m, fresh := anilist.Media{}, false
	if s.importer != nil {
		m, fresh = s.importer.Finished(r.Context(), id)
	}
	if !fresh {
		media, err := s.anilist.MediaByIDs(r.Context(), []int{id})
		failed := err != nil && !errors.Is(err, anilist.ErrIncomplete)
		if failed {
			if m, ok := s.savedAnime(r.Context(), id); ok {
				s.log.Warn("anilist unreachable, showing the saved record", "anime", id, "err", err)
				media, failed = []anilist.Media{m}, false
			}
		}
		if failed || len(media) == 0 {
			s.log.Warn("anime lookup failed", "anime", id, "returned", len(media), "err", err)
			return nil, false, failed
		}
		m = media[0]

		if s.importer != nil {
			if _, err := s.importer.Save(r.Context(), media); err != nil {
				s.log.Warn("save anime", "anime", id, "err", err)
			}
		}
	}

	var romaji string
	if m.Title.Romaji != nil {
		romaji = *m.Title.Romaji
	}
	body = map[string]any{
		"id":       id,
		"source":   "anilist",
		"anime":    m,
		"title":    store.PickTitle(titles, romaji, m.Title.English),
		"onList":   listed,
		"progress": progress,
		"sync":     syncable(true, m.IDMal != nil),
	}
	if m.IDMal != nil {
		body["malId"] = *m.IDMal
	}
	if d, ok := s.store.ShowDelay(r.Context(), id); ok {
		body["delay"] = map[string]any{"episode": d.Episode, "wasAt": d.WasAt, "nowAt": d.NowAt}
	}
	return body, true, false
}

func (s *Server) savedAnime(ctx context.Context, id int) (anilist.Media, bool) {
	a, at, ok, err := s.store.AnimeRecord(ctx, id)
	if err != nil || !ok {
		return anilist.Media{}, false
	}
	anilist.NoteSaved(ctx, at)
	return library.FromRecord(a), true
}

func syncable(anilist, mal bool) map[string]bool {
	return map[string]bool{"anilist": anilist, "mal": mal}
}
