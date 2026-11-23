package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"yoru/internal/disk"
	"yoru/internal/library"
)

type playBody struct {
	AnimeID  int    `json:"animeId"`
	Episode  int    `json:"episode"`
	Season   int    `json:"season"`
	InfoHash string `json:"infoHash"`
	External bool   `json:"external"`
	AllowRaw bool   `json:"allowRaw"`
	Audio    string `json:"audio,omitempty"`
}

func (s *Server) play(w http.ResponseWriter, r *http.Request) {
	if s.playback == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{
			"error": "torrent engine unavailable",
		})
		return
	}

	var body playBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		send(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	if body.AnimeID == 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "animeId is required"})
		return
	}
	prefs := s.preferences(r.Context(), body.AnimeID)
	chosen := false
	switch body.Audio {
	case "sub", "dub", "either":
		prefs.Audio, chosen = body.Audio, true
	}

	session, err := s.playback.Start(r.Context(), library.PlayRequest{
		AnimeID:     body.AnimeID,
		Episode:     body.Episode,
		Season:      body.Season,
		InfoHash:    body.InfoHash,
		External:    body.External && fromHost(r),
		AllowRaw:    body.AllowRaw,
		AudioChosen: chosen,
		Prefs:       prefs,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) && r.Context().Err() != nil {
			s.log.Debug("play left", "anime", body.AnimeID, "episode", body.Episode)
		} else {
			s.log.Error("play", "anime", body.AnimeID, "episode", body.Episode, "err", err)
		}

		out := map[string]any{"error": err.Error()}
		var missing *library.NoRelease
		if errors.As(err, &missing) && missing.RawOnly {
			out["rawAvailable"] = true
			out["rawTitle"] = missing.RawTitle
		}
		if missing != nil && (missing.Unaired || missing.RawOnly) {
			out["notOut"] = true
		}
		send(w, http.StatusBadGateway, out)
		return
	}
	send(w, http.StatusOK, session)
}

func (s *Server) playStage(w http.ResponseWriter, r *http.Request) {
	animeID, _ := strconv.Atoi(r.URL.Query().Get("id"))
	episode, _ := strconv.Atoi(r.URL.Query().Get("episode"))
	stage, _ := s.playback.Stage(animeID, episode)
	send(w, http.StatusOK, stage)
}

func (s *Server) cacheStatus(w http.ResponseWriter, r *http.Request) {
	usage, err := s.store.CacheUsage(r.Context())
	if err != nil {
		s.fail(w, "cache usage", err)
		return
	}
	if free, total, drive, err := disk.Space(s.cfg.CacheDir); err == nil {
		usage.FreeBytes, usage.Drive = &free, drive
		if total > 0 {
			usage.TotalBytes = &total
		}
	}
	send(w, http.StatusOK, usage)
}

func (s *Server) stopPlayback(w http.ResponseWriter, r *http.Request) {
	if s.playback != nil {
		s.playback.StopPlayers()
	} else if s.player != nil {
		s.player.Stop()
	}

	var body struct {
		AnimeID int `json:"animeId"`
		Episode int `json:"episode"`
	}
	json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body)
	if s.playback != nil {
		if animeID, episode, _ := s.playback.External(); animeID > 0 {
			body.AnimeID, body.Episode = animeID, episode
		}
	}
	if body.AnimeID > 0 && s.playback != nil {
		s.playback.Suspend(r.Context(), body.AnimeID, body.Episode)
	}
	send(w, http.StatusOK, map[string]any{"stopped": true})
}

func (s *Server) externalPlayer(w http.ResponseWriter, r *http.Request) {
	if s.playback == nil {
		send(w, http.StatusOK, map[string]any{})
		return
	}
	animeID, episode, failed := s.playback.External()
	out := map[string]any{"animeId": animeID, "episode": episode}
	if animeID > 0 {
		out["title"] = s.store.ShownTitle(r.Context(), animeID)
	}
	if failed != "" {
		out["failed"] = failed
	}
	send(w, http.StatusOK, out)
}
