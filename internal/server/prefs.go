package server

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"yoru/internal/library"
	"yoru/internal/score"
	"yoru/internal/sound"
	"yoru/internal/store"
	"yoru/internal/torrent"
)

// Values are validated against the known set so a typo cannot silently disable
// a feature: an unrecognised Anime4K mode would otherwise fail at playback.
var allowed = map[string][]string{
	"playback.player":          store.Players,
	"playback.anime4k_mode":    store.Anime4KModes,
	"playback.anime4k_size":    store.Anime4KSizes,
	"audio.prefer":             store.AudioPrefs,
	"display.titles":           store.TitleModes,
	"notify.releases":          {"sub", "dub", "both"},
	"cache.autodelete":         store.AutoDeletes,
	"playback.voice":           sound.Levels,
	"playback.boost":           sound.Levels,
	"downloads.while_watching": {"pause", "continue"},
	"display.speed":            {"mbps", "mbs"},
	"quality.prefer":           {"best", "720p", "480p"},
	"downloads.upload":         {torrent.UploadWhileDownloading, torrent.UploadNever},
	"window.mode":              {"fullscreen", "maximized"},
	"window.on_close":          {"exit", "tray"},
	// 2, 3, 5 or 10 GiB an episode, or none.
	"quality.max_auto_bytes": {"2147483648", "3221225472", "5368709120", "10737418240", "0"},
	"display.spoilers":       {store.SpoilersOff, store.SpoilersAfterNext, store.SpoilersUnwatched},
	// Episodes auto next plays unattended before asking whether anyone is watching; 0 never asks.
	"playback.still_watching": {"0", "2", "3", "5"},
	// The share of an episode that counts as watching it, or where its ending starts.
	"sync.progress_at":  {"0.8", "0.85", "0.9", "0.95", store.EndingThreshold},
	"subtitle.with_dub": {"signs", "off", "same"},
	// Hours auto-download holds a new episode for a preferred group or a higher resolution; 0 takes the first.
	"autodownload.wait_hours": {"0", "1", "3", "6", "12"},
}

// numeric settings take whole numbers; a typo would read as zero.
var numeric = map[string]bool{
	"cache.budget_bytes": true, "notify.poll_seconds": true, "cache.prefetch_count": true, "downloads.limit_bytes": true,
}

func isBool(def string) bool { return def == "true" || def == "false" }

func (s *Server) getPrefs(w http.ResponseWriter, r *http.Request) {
	animeID, _ := strconv.Atoi(r.URL.Query().Get("anime"))

	prefs, err := s.store.Prefs(r.Context(), animeID)
	if err != nil {
		s.fail(w, "prefs", err)
		return
	}

	body := map[string]any{"effective": prefs.All(), "defaults": store.Defaults}
	// Negative ids are MAL-only shows: still a show, not the globals.
	if animeID != 0 {
		overrides, err := s.store.AnimePrefs(r.Context(), animeID)
		if err != nil {
			s.fail(w, "anime prefs", err)
			return
		}
		body["overrides"] = overrides
	}
	send(w, http.StatusOK, body)
}

// Without an anime id this sets the default; with one, a per-show override (empty removes it).
func (s *Server) setPref(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AnimeID int    `json:"animeId"`
		Key     string `json:"key"`
		Value   string `json:"value"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		send(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}

	if _, known := store.Defaults[body.Key]; !known {
		send(w, http.StatusBadRequest, map[string]any{"error": "unknown setting " + body.Key})
		return
	}
	// Which network this machine's torrents use is the host's to say, as its firewall is.
	if hostSettings[body.Key] && !fromHost(r) {
		send(w, http.StatusForbidden, map[string]any{"error": "change this from the host machine"})
		return
	}
	// Booleans are read as == "true", so "True" or "yes" would silently mean off.
	if isBool(store.Defaults[body.Key]) {
		if body.Value != "" && body.Value != "true" && body.Value != "false" {
			send(w, http.StatusBadRequest, map[string]any{
				"error": "invalid value for " + body.Key, "allowed": []string{"true", "false"},
			})
			return
		}
	} else if valid, ok := allowed[body.Key]; ok && body.Value != "" && !slices.Contains(valid, body.Value) {
		send(w, http.StatusBadRequest, map[string]any{
			"error": "invalid value for " + body.Key, "allowed": valid,
		})
		return
	} else if numeric[body.Key] && body.Value != "" {
		if _, err := strconv.ParseInt(body.Value, 10, 64); err != nil {
			send(w, http.StatusBadRequest, map[string]any{"error": "invalid value for " + body.Key + ": a whole number"})
			return
		}
	}

	var err error
	if body.AnimeID != 0 {
		err = s.store.SetAnimePref(r.Context(), body.AnimeID, body.Key, body.Value)
	} else {
		err = s.store.SetSetting(r.Context(), body.Key, body.Value)
	}
	if err != nil {
		s.fail(w, "set pref", err)
		return
	}
	if body.AnimeID == 0 && limitSettings[body.Key] {
		s.applyLimits(r.Context())
	}
	if body.AnimeID == 0 && strings.HasPrefix(body.Key, "discord.") && s.presence != nil {
		if prefs, err := s.store.Prefs(r.Context(), 0); err == nil {
			s.presence.Apply(prefs)
		}
	}
	send(w, http.StatusOK, map[string]any{"ok": true})
}

// limitSettings are the engine's; hostSettings change only from the machine Yoru runs on.
var (
	limitSettings = map[string]bool{"downloads.limit_bytes": true, "downloads.upload": true, "downloads.adapter": true}
	// The Discord card is the host's account, as the network is the host's machine.
	hostSettings = map[string]bool{"downloads.adapter": true, "discord.enabled": true, "discord.everywhere": true,
		"power.awake_watching": true, "power.awake_downloading": true}
)

// applyLimits hands the engine the download cap, upload choice and network as saved; they apply at once.
func (s *Server) applyLimits(ctx context.Context) {
	prefs, err := s.store.Prefs(ctx, 0)
	if err != nil {
		s.log.Warn("read limits", "err", err)
		return
	}
	if s.engine != nil {
		s.engine.SetLimits(library.TorrentLimits(prefs))
	}
}

func (s *Server) bookmarks(w http.ResponseWriter, r *http.Request) {
	page, err := s.store.Bookmarks(r.Context(), store.ParsePaging(r.URL.Query(), 60, 200))
	if err != nil {
		s.fail(w, "bookmarks", err)
		return
	}
	send(w, http.StatusOK, page)
}

func (s *Server) setBookmark(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AnimeID int `json:"animeId"`
		store.BookmarkPatch
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil || body.AnimeID == 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "animeId is required"})
		return
	}
	if err := s.store.PatchBookmark(r.Context(), body.AnimeID, body.BookmarkPatch); err != nil {
		s.fail(w, "set bookmark", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) recent(w http.ResponseWriter, r *http.Request) {
	page, err := s.store.RecentlyWatched(r.Context(), store.ParsePaging(r.URL.Query(), 30, 100))
	if err != nil {
		s.fail(w, "recently watched", err)
		return
	}
	send(w, http.StatusOK, page)
}

func (s *Server) notifications(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	unread := r.URL.Query().Get("unread") == "true"

	items, err := s.store.Notifications(r.Context(), unread, limit)
	if err != nil {
		s.fail(w, "notifications", err)
		return
	}
	count, _ := s.store.UnreadCount(r.Context())
	send(w, http.StatusOK, map[string]any{"items": items, "unread": count})
}

func (s *Server) markRead(w http.ResponseWriter, r *http.Request) {
	id, ok := notificationID(w, r)
	if !ok {
		return
	}
	if err := s.store.MarkRead(r.Context(), id); err != nil {
		s.fail(w, "mark read", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"ok": true})
}

// notificationID reads ?id=, refusing a bad one, since zero would clear every notification.
func notificationID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.URL.Query().Get("id")
	if raw == "" {
		return 0, true
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "invalid id"})
		return 0, false
	}
	return id, true
}

// deleteNotification removes one by id, or all of them when none is given.
func (s *Server) deleteNotification(w http.ResponseWriter, r *http.Request) {
	id, ok := notificationID(w, r)
	if !ok {
		return
	}

	removed, err := s.store.DeleteNotification(r.Context(), id)
	if err != nil {
		s.fail(w, "delete notification", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"removed": removed})
}

func (s *Server) setFollow(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AnimeID int  `json:"animeId"`
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil || body.AnimeID == 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "animeId is required"})
		return
	}

	if err := s.store.SetFollow(r.Context(), body.AnimeID, body.Enabled); err != nil {
		s.fail(w, "follow", err)
		return
	}
	if body.Enabled {
		s.hydrate(r.Context(), []int{body.AnimeID})
	}
	send(w, http.StatusOK, map[string]any{"following": body.Enabled})
}

// setNotify turns a show's new-episode alerts on or off, whatever turned them on: a follow, Watching, auto.
func (s *Server) setNotify(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	var body struct {
		On bool `json:"on"`
	}
	if id == 0 || !decode(w, r, &body) {
		if id == 0 {
			send(w, http.StatusBadRequest, map[string]any{"error": "a show is required"})
		}
		return
	}
	if err := s.store.SetNotify(r.Context(), id, body.On); err != nil {
		s.fail(w, "notify", err)
		return
	}
	if body.On {
		s.hydrate(r.Context(), []int{id})
	}
	s.sendFollow(w, r, id)
}

// wantEpisode marks one episode to download when the release watch finds it; 0 takes the mark off.
func (s *Server) wantEpisode(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	var body struct {
		Episode int `json:"episode"`
	}
	if id == 0 || !decode(w, r, &body) {
		if id == 0 {
			send(w, http.StatusBadRequest, map[string]any{"error": "a show is required"})
		}
		return
	}
	value := ""
	if body.Episode > 0 {
		value = strconv.Itoa(body.Episode)
		if err := s.store.EnsureAnime(r.Context(), id); err != nil {
			s.fail(w, "want episode", err)
			return
		}
		s.hydrate(r.Context(), []int{id})
	}
	if err := s.store.SetAnimePref(r.Context(), id, "autodownload.want", value); err != nil {
		s.fail(w, "want episode", err)
		return
	}
	s.sendFollow(w, r, id)
}

func (s *Server) sendFollow(w http.ResponseWriter, r *http.Request, id int) {
	f, err := s.store.FollowOf(r.Context(), id)
	if err != nil {
		s.fail(w, "follow state", err)
		return
	}
	send(w, http.StatusOK, f)
}

// hydrate fills in placeholder rows, which otherwise sit titled "Unknown".
func (s *Server) hydrate(ctx context.Context, ids []int) {
	if s.importer == nil {
		return
	}
	missing, err := s.store.Unhydrated(ctx, ids)
	if err != nil || len(missing) == 0 {
		return
	}
	if _, err := s.importer.Hydrate(ctx, missing); err != nil {
		s.log.Warn("hydrate anime", "count", len(missing), "err", err)
	}
}

// previewReleases says per show what auto-download would take next and why, taking nothing.
func (s *Server) previewReleases(w http.ResponseWriter, r *http.Request) {
	if s.watcher == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "watcher unavailable"})
		return
	}
	rows, err := s.watcher.Preview(r.Context())
	if err != nil {
		s.fail(w, "preview auto-download", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"items": rows})
}

// releaseWaits lists the new episodes auto-download holds for a better release: what each waits for, and until.
func (s *Server) releaseWaits(w http.ResponseWriter, r *http.Request) {
	waits, err := s.store.Waits(r.Context(), 0)
	if err != nil {
		s.fail(w, "release waits", err)
		return
	}
	type item struct {
		store.ReleaseWait
		Until int64  `json:"until"`
		Wants string `json:"wants"`
	}
	out := []item{}
	for _, x := range waits {
		prefs, err := s.store.Prefs(r.Context(), x.AnimeID)
		if err != nil {
			continue
		}
		hours := time.Duration(prefs.Int64("autodownload.wait_hours")) * time.Hour
		out = append(out, item{ReleaseWait: x, Until: x.FirstSeen.Add(hours).Unix(), Wants: score.Wanted(library.Preferences(prefs))})
	}
	send(w, http.StatusOK, map[string]any{"items": out})
}

// takeWaitingNow stops holding an episode and queues it: the queue takes the best release there is.
func (s *Server) takeWaitingNow(w http.ResponseWriter, r *http.Request) {
	animeID, _ := strconv.Atoi(r.PathValue("anime"))
	episode, _ := strconv.Atoi(r.PathValue("episode"))
	if animeID == 0 || episode <= 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "anime and episode are needed"})
		return
	}
	if _, err := s.store.Enqueue(r.Context(), animeID, 0, []int{episode}); err != nil {
		s.fail(w, "queue episode", err)
		return
	}
	if err := s.store.EndWait(r.Context(), animeID, episode); err != nil {
		s.fail(w, "end wait", err)
		return
	}
	s.downloader.Wake()
	send(w, http.StatusOK, map[string]any{"queued": true})
}

func (s *Server) checkReleases(w http.ResponseWriter, r *http.Request) {
	if s.watcher == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "watcher unavailable"})
		return
	}
	rep, err := s.watcher.Poll(r.Context())
	if err != nil {
		s.fail(w, "check releases", err)
		return
	}
	send(w, http.StatusOK, rep)
}
