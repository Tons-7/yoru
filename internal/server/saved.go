package server

import (
	"crypto/sha1"
	"encoding/hex"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"time"

	"yoru/internal/store"
	"yoru/web"
)

// appFiles lists the built app, for a phone to keep and open Yoru with the PC off.
func (s *Server) appFiles(w http.ResponseWriter, _ *http.Request) {
	assets, ok := web.FS()
	if !ok {
		send(w, http.StatusNotFound, map[string]any{"error": "no frontend in this build"})
		return
	}
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		s.fail(w, "app files", err)
		return
	}
	sum := sha1.Sum(index)
	files := []string{"/"}
	err = fs.WalkDir(assets, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path == "sw.js" || path == "index.html" {
			return err
		}
		files = append(files, "/"+path)
		return nil
	})
	if err != nil {
		s.fail(w, "app files", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"version": hex.EncodeToString(sum[:6]), "files": files})
}

// savedReady: the PC has the episode whole for a phone's copy; asked while waiting, so it opens nothing.
func (s *Server) savedReady(w http.ResponseWriter, r *http.Request) {
	animeID, _ := strconv.Atoi(r.URL.Query().Get("anime"))
	episode, _ := strconv.Atoi(r.URL.Query().Get("episode"))
	ctx := r.Context()
	if f, err := s.store.LocalEpisode(ctx, animeID, episode); err == nil && f.ID != 0 {
		if _, err := os.Stat(f.Path); err == nil {
			send(w, http.StatusOK, map[string]any{"ready": true})
			return
		}
	}
	rec, found, err := s.store.TorrentForEpisode(ctx, animeID, strconv.Itoa(episode))
	if err != nil {
		s.fail(w, "saved ready", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"ready": found && s.torrentWhole(ctx, rec), "downloading": found})
}

type savedWatch struct {
	AnimeID  int     `json:"animeId"`
	Episode  int     `json:"episode"`
	Position float64 `json:"position"`
	Duration float64 `json:"duration"`
	Played   float64 `json:"played"`
	At       int64   `json:"at"`
}

// savedProgress takes what was watched on a phone's saved copies while Yoru was out of reach.
func (s *Server) savedProgress(w http.ResponseWriter, r *http.Request) {
	var body struct {
		// The phone's clock, so its times can be read on this one's.
		Now   int64        `json:"now"`
		Items []savedWatch `json:"items"`
	}
	if !decode(w, r, &body) {
		return
	}
	var skew time.Duration
	if body.Now > 0 {
		skew = time.Since(time.UnixMilli(body.Now))
	}
	ctx := r.Context()
	watched, failed := []savedWatch{}, []savedWatch{}
	for _, item := range body.Items {
		if item.AnimeID == 0 || item.Episode <= 0 {
			continue
		}
		// Watched here since: the phone's older place would move it back.
		at := time.UnixMilli(item.At).Add(skew + time.Minute).Unix()
		if last, err := s.store.PlayedAt(ctx, item.AnimeID, epKey(item.Episode)); err == nil && last > at {
			continue
		}
		done, err := s.saveState(ctx, item.Episode, store.PlaybackState{AnimeID: item.AnimeID, Position: item.Position,
			Duration: item.Duration, Played: item.Played, Catchup: true})
		if err != nil {
			s.log.Warn("saved progress", "anime", item.AnimeID, "episode", item.Episode, "err", err)
			failed = append(failed, item)
			continue
		}
		if done {
			watched = append(watched, item)
		}
	}
	send(w, http.StatusOK, map[string]any{"saved": len(body.Items) - len(failed), "watched": watched, "failed": failed})
}
