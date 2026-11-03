package server

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"yoru/internal/library"
	"yoru/internal/proc"
	"yoru/internal/sound"
	"yoru/internal/store"
	"yoru/internal/torrent"
	"yoru/internal/transcode"
)

func (s *Server) streamOpen(w http.ResponseWriter, r *http.Request) {
	if s.streams == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "transcoder unavailable"})
		return
	}

	animeID, _ := strconv.Atoi(r.URL.Query().Get("id"))
	episode, _ := strconv.Atoi(r.URL.Query().Get("episode"))
	if animeID == 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "id is required"})
		return
	}

	source := r.URL.Query().Get("source")
	if source == "" {
		send(w, http.StatusBadRequest, map[string]any{
			"error": "source is required; start playback first to obtain a stream url",
		})
		return
	}

	if path, ok := s.localSource(r.Context(), source); ok {
		source = path
	} else if !s.engineSource(source) {
		send(w, http.StatusBadRequest, map[string]any{"error": "unrecognised source"})
		return
	}

	if !s.streams.Installed() {
		send(w, http.StatusServiceUnavailable, map[string]any{
			"error": "Watching in the browser needs the transcoder, which isn't installed.",
			"code":  "no-transcoder",
		})
		return
	}

	id := sessionID(animeID, episode)
	if s.playback != nil && s.engineSource(source) {
		s.playback.SetStage(animeID, episode, library.Stage{Step: "start"})
		defer s.playback.ClearStage(animeID, episode)
	}
	want := s.streamWant(r.Context(), animeID, r.URL.Query().Get("quality"))
	want.Keep = r.URL.Query().Get("keep") == "1"
	session, err := s.streams.OpenFor(r.Context(), id, source,
		s.readablePath(r.Context(), animeID, episode, source), transcode.ParseCaps(r.URL.Query().Get("caps")), want)
	if err != nil {
		if s.playback != nil {
			s.playback.ResumeYielded(context.WithoutCancel(r.Context()))
		}
		s.fail(w, "open stream", err)
		return
	}
	id = session.ID
	if r.Context().Err() != nil {
		if s.playback != nil {
			s.playback.ResumeYielded(context.WithoutCancel(r.Context()))
		}
		return
	}

	if rec, found, err := s.store.TorrentForEpisode(r.Context(), animeID, strconv.Itoa(episode)); err == nil && found {
		if err := s.store.PinPlaying(r.Context(), animeID, strconv.Itoa(episode), rec.InfoHash, rec.FileIndex); err != nil {
			s.log.Warn("pin playing episode", "anime", animeID, "episode", episode, "err", err)
		}
	}

	if s.holdWhileWatching(r.Context(), session) {
		s.downloader.Hold(id)
	}

	if want := r.URL.Query().Get("audio"); session.ApplyAudioPreference(want) {
		session.SetAudioTrack(transcode.ChooseAudio(session.Info, want))
	}
	if n, err := strconv.Atoi(r.URL.Query().Get("track")); err == nil {
		session.SetAudioTrack(n)
	}
	if o, any := soundFromQuery(r, session.Sound()); any {
		session.SetSound(o)
	}

	if !want.Keep {
		go s.prestart(session, animeID, episode)
	}

	base := "/api/stream/" + id
	body := map[string]any{
		"id":         id,
		"playlist":   base + "/playlist.m3u8",
		"duration":   session.Info.Duration,
		"plan":       session.CurrentPlan(),
		"video":      session.Info.Video,
		"audio":      session.Info.Audio,
		"audioTrack": session.AudioTrack(),
		"sound":      session.Sound(),
		"complete":   s.sourceComplete(r.Context(), session),
	}

	if s.rebind == nil || s.bound() {
		airplay := grant{Source: castPrefix + id, Expires: time.Now().Add(externalFor).Unix()}
		body["airplay"] = s.tvBase(r) + "/c/" + s.signGrant(airplay) + "/master.m3u8"
	}

	tracks := s.subtitleTracks(session, base)
	body["subtitles"] = tracks
	body["fonts"] = []any{}
	if slices.ContainsFunc(tracks, func(t transcode.Track) bool { return !t.Pictures }) {
		body["fontsUrl"] = base + "/fonts"
		s.extractFontsAsync(session, base)
	}
	if len(tracks) == 0 && len(session.Info.Subtitles) > 0 {
		body["imageSubtitles"] = true
	}

	send(w, http.StatusOK, body)
}

func (s *Server) streamWant(ctx context.Context, animeID int, quality string) transcode.Want {
	var want transcode.Want
	if step, ok := transcode.Steps[quality]; ok {
		want.Height, want.Rate = step[0], step[1]
	}
	if prefs, err := s.store.Prefs(ctx, animeID); err == nil {
		want.Soften = prefs.Bool("playback.soften_flashing")
	}
	return want
}

func (s *Server) prestart(session *transcode.Session, animeID, episode int) {
	ctx, cancel := detached(30 * time.Second)
	defer cancel()

	from := s.resumeSegment(ctx, session, animeID, episode)
	if err := session.Prestart(from); err != nil {
		s.log.Debug("prestart encoder", "session", session.ID, "from", from, "err", err)
	}
}

func (s *Server) resumeSegment(ctx context.Context, session *transcode.Session, animeID, episode int) int {
	if at, err := s.store.ResumeAt(ctx, animeID, strconv.Itoa(episode)); err == nil && at > 0 {
		return session.SegmentAt(at)
	}
	return 0
}

func (s *Server) subtitleTracks(session *transcode.Session, base string) []transcode.Track {
	tracks := []transcode.Track{}
	for _, sub := range session.Info.Subtitles {
		pictures := transcode.PictureSubtitle(sub.Codec)
		if !transcode.Renderable(sub.Codec) && !pictures {
			continue
		}
		url := fmt.Sprintf("%s/subtitle/%d", base, sub.Index)
		if pictures {
			url = fmt.Sprintf("%s/pgs/%d", base, sub.Index)
		}
		tracks = append(tracks, transcode.Track{
			Index:    sub.Index,
			Language: sub.Language,
			Title:    sub.Title,
			Codec:    sub.Codec,
			Default:  sub.Default,
			Forced:   sub.Forced,
			URL:      url,
			Pictures: pictures,
		})
	}
	for i, side := range sidecars(session.Source) {
		tracks = append(tracks, transcode.Track{
			Index:    sidecarOffset + i,
			Language: side.language,
			Title:    side.title,
			Codec:    side.codec,
			URL:      fmt.Sprintf("%s/subtitle/%d", base, sidecarOffset+i),
		})
	}
	return tracks
}

const sidecarOffset = 1000

type sidecar struct{ path, language, title, codec string }

var sidecarCodecs = map[string]string{".ass": "ass", ".ssa": "ssa", ".srt": "subrip", ".vtt": "webvtt"}

func sidecars(video string) []sidecar {
	if video == "" || strings.Contains(video, "://") {
		return nil
	}
	dir, file := filepath.Split(video)
	stem := strings.ToLower(strings.TrimSuffix(file, filepath.Ext(file)))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []sidecar
	for _, e := range entries {
		codec := sidecarCodecs[strings.ToLower(filepath.Ext(e.Name()))]
		if codec == "" || e.IsDir() {
			continue
		}
		rest, ok := strings.CutPrefix(strings.ToLower(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))), stem)
		if !ok || (rest != "" && rest[0] != '.') {
			continue
		}
		side := sidecar{path: filepath.Join(dir, e.Name()), codec: codec}
		label := strings.TrimPrefix(rest, ".")
		if lang, _, _ := strings.Cut(label, "."); len(lang) >= 2 && len(lang) <= 3 || lang == "english" {
			side.language = lang
		}
		if side.language == "" || label != side.language {
			side.title = label
		}
		out = append(out, side)
	}
	return out
}

func (s *Server) extractFontsAsync(session *transcode.Session, base string) {
	if s.subtitles == nil {
		return
	}
	id, key := session.ID, session.Dir()

	s.fontsMu.Lock()
	if _, known := s.fonts[key]; known {
		s.fontsMu.Unlock()
		return
	}
	if _, running := s.fontJobs[key]; running {
		s.fontsMu.Unlock()
		return
	}
	s.fontJobs[key] = struct{}{}
	s.fontsMu.Unlock()

	go func() {
		ctx, cancel := detached(5 * time.Minute)
		defer cancel()

		found, err := s.subtitles.ExtractFonts(ctx, s.readable(ctx, session), session.Dir(), session.Info.Attachments)
		if err != nil {
			s.log.Info("fonts not ready", "session", id, "err", err)
			s.fontsMu.Lock()
			delete(s.fontJobs, key)
			s.fontsMu.Unlock()
			return
		}

		fonts := []transcode.FontFile{}
		for _, f := range found {
			f.URL = base + "/font/" + f.Name
			fonts = append(fonts, f)
		}

		s.fontsMu.Lock()
		if _, live := s.fontJobs[key]; live {
			s.fonts[key] = fonts
			delete(s.fontJobs, key)
		}
		s.fontsMu.Unlock()

		s.log.Debug("fonts extracted", "session", id, "count", len(fonts))
	}()
}

func (s *Server) streamFonts(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	session, ok := s.streams.Get(id)
	if !ok {
		send(w, http.StatusOK, map[string]any{"ready": false, "extracting": false, "fonts": []any{}})
		return
	}

	s.fontsMu.Lock()
	fonts, done := s.fonts[session.Dir()]
	_, running := s.fontJobs[session.Dir()]
	s.fontsMu.Unlock()

	if !done && !running {
		s.extractFontsAsync(session, "/api/stream/"+id)
		running = true
	}
	if !done {
		send(w, http.StatusOK, map[string]any{
			"ready": false, "extracting": running, "fonts": []any{},
		})
		return
	}
	send(w, http.StatusOK, map[string]any{"ready": true, "fonts": fonts})
}

func (s *Server) engineSource(source string) bool {
	u, err := url.Parse(source)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	return s.playback != nil && strings.EqualFold(u.Host, s.playback.EngineAddr())
}

var localRoute = regexp.MustCompile(`^/api/local/(\d+)/stream$`)

func (s *Server) localSource(ctx context.Context, source string) (string, bool) {
	m := localRoute.FindStringSubmatch(source)
	if m == nil {
		return "", false
	}
	id, _ := strconv.Atoi(m[1])
	f, err := s.store.LocalFile(ctx, id)
	if err != nil || f.ID == 0 || f.Path == "" {
		return "", false
	}
	return f.Path, true
}

func (s *Server) readable(ctx context.Context, session *transcode.Session) string {
	animeID, episode, ok := parseSessionID(session.ID)
	if !ok {
		return session.Source
	}
	return s.readablePath(ctx, animeID, episode, session.Source)
}

func (s *Server) readablePath(ctx context.Context, animeID, episode int, fallback string) string {
	if !isTorrentSource(fallback) {
		return fallback
	}
	rec, found, err := s.store.TorrentForEpisode(ctx, animeID, strconv.Itoa(episode))
	if err != nil || !found {
		return fallback
	}
	path := s.cachePath(rec)
	if path == "" || s.cache != nil && s.cache.Downloaded(ctx, rec.InfoHash) == 0 {
		return fallback
	}
	return path
}

func (s *Server) cachePath(rec store.TorrentRecord) string {
	if rec.FilePath == "" {
		return ""
	}
	var inPack []string
	for part := range strings.SplitSeq(rec.FilePath, "/") {
		inPack = append(inPack, torrent.SafeName(part))
	}
	var candidates []string
	for _, folder := range torrent.DiskNames(rec.Name, rec.InfoHash) {
		candidates = append(candidates, filepath.Join(append([]string{s.cfg.CacheDir, folder}, inPack...)...))
	}
	for _, file := range torrent.DiskNames(rec.FilePath, rec.InfoHash) {
		candidates = append(candidates, filepath.Join(s.cfg.CacheDir, file))
	}
	for _, p := range candidates {
		if !torrent.Within(s.cfg.CacheDir, p) {
			continue
		}
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

func (s *Server) streamPlaylist(w http.ResponseWriter, r *http.Request) {
	session, ok := s.stream(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(session.Playlist()))
}

func (s *Server) streamInit(w http.ResponseWriter, r *http.Request) {
	session, ok := s.stream(w, r)
	if !ok {
		return
	}

	from := 0
	if !session.Kept() {
		animeID, episode, _ := parseSessionID(session.ID)
		from = s.resumeSegment(r.Context(), session, animeID, episode)
	}
	path, err := session.WaitInit(r.Context(), from, 60*time.Second)
	if err != nil {
		if !unconvertible(w, err) {
			s.fail(w, "stream init", err)
		}
		return
	}
	serveFile(w, r, path, "video/mp4")
}

func unconvertible(w http.ResponseWriter, err error) bool {
	u, ok := errors.AsType[*transcode.Unconvertible](err)
	if ok {
		send(w, http.StatusUnprocessableEntity, map[string]any{"error": u.Error(), "code": "unconvertible"})
	}
	return ok
}

func (s *Server) streamSegment(w http.ResponseWriter, r *http.Request) {
	session, ok := s.stream(w, r)
	if !ok {
		return
	}

	n, err := strconv.Atoi(strings.TrimSuffix(r.PathValue("segment"), ".mp4"))
	if err != nil || n < 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "invalid segment"})
		return
	}
	if session.Segments > 0 && n >= session.Segments {
		send(w, http.StatusNotFound, map[string]any{"error": "no such segment"})
		return
	}

	if err := session.WaitSegment(r.Context(), n, 90*time.Second); err != nil {
		s.log.Warn("segment", "session", session.ID, "n", n, "err", err)
		if !unconvertible(w, err) {
			http.Error(w, "segment unavailable", http.StatusGatewayTimeout)
		}
		return
	}
	seg, err := session.OpenSegment(n)
	if err != nil {
		s.log.Warn("segment", "session", session.ID, "n", n, "err", err)
		http.Error(w, "segment unavailable", http.StatusGatewayTimeout)
		return
	}
	defer seg.Close()
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "", seg.Modified, seg)
}

func (s *Server) streamSubtitle(w http.ResponseWriter, r *http.Request) {
	session, ok := s.stream(w, r)
	if !ok {
		return
	}
	if s.subtitles == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "subtitles unavailable"})
		return
	}

	index, err := strconv.Atoi(r.PathValue("track"))
	if err != nil {
		send(w, http.StatusBadRequest, map[string]any{"error": "invalid track"})
		return
	}
	if index >= sidecarOffset {
		sides := sidecars(session.Source)
		if index-sidecarOffset >= len(sides) {
			http.NotFound(w, r)
			return
		}
		side := sides[index-sidecarOffset]
		dir := filepath.Join(session.Dir(), fmt.Sprintf("side-%d", index-sidecarOffset))
		path, err := s.subtitles.Extract(r.Context(), side.path, dir, 0, side.codec, true)
		if err != nil {
			s.fail(w, "subtitle file", err)
			return
		}
		track, err := os.ReadFile(path)
		if err != nil {
			s.fail(w, "subtitle file", err)
			return
		}
		w.Header().Set("X-Yoru-Complete", "true")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(track)
		return
	}

	var codec string
	for _, sub := range session.Info.Subtitles {
		if sub.Index == index {
			codec = sub.Codec
		}
	}
	if codec == "" {
		http.NotFound(w, r)
		return
	}

	dir := session.Dir()
	local := s.readable(r.Context(), session)
	complete := s.sourceComplete(r.Context(), session)

	at, _ := strconv.ParseFloat(r.URL.Query().Get("at"), 64)

	liveStart, live := session.LiveSubtitles()
	live = live && !complete
	var path string
	have := false
	if live {
		path, err = s.subtitles.MergeLive(dir, index, codec)
		have = err == nil && s.subtitles.Cues(dir, index, codec) > 0
		if !have {
			if p, ok := s.subtitles.AwaitLive(r.Context(), dir, index, codec); ok {
				path, err, have = p, nil, true
			}
		}
	}

	if !have {
		path, err = s.subtitles.Extract(r.Context(), local, dir, index, codec, complete)

		if (err != nil || s.subtitles.Cues(dir, index, codec) == 0) && local != session.Source && !live {
			if p, retry := s.subtitles.Extract(r.Context(), session.Source, dir, index, codec, false); retry == nil {
				path, err = p, nil
			}
		}
	}
	if at > 0 && !complete && !(live && transcode.LiveCovers(liveStart, at)) {
		if p, aroundErr := s.subtitles.ExtractAround(r.Context(), local, dir, index, codec, at); aroundErr == nil {
			path, err = p, nil
		} else {
			s.log.Debug("subtitle around playhead", "session", session.ID, "at", at, "err", aroundErr)
		}
	}
	if live && !have {
		if p, mergeErr := s.subtitles.MergeLive(dir, index, codec); mergeErr == nil && s.subtitles.Cues(dir, index, codec) > 0 {
			path, err = p, nil
		}
	}
	if err != nil {
		s.fail(w, "extract subtitle", err)
		return
	}

	track, readErr := os.ReadFile(path)
	if readErr != nil {
		s.fail(w, "extract subtitle", fmt.Errorf("track %d: %w", index, readErr))
		return
	}
	w.Header().Set("X-Yoru-Complete", strconv.FormatBool(complete))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(track)
}

func (s *Server) streamPictureSubtitle(w http.ResponseWriter, r *http.Request) {
	session, ok := s.stream(w, r)
	if !ok {
		return
	}
	index, err := strconv.Atoi(r.PathValue("track"))
	if err != nil || !transcode.PictureSubtitle(session.Info.SubtitleCodec(index)) {
		http.NotFound(w, r)
		return
	}
	if !s.sourceComplete(r.Context(), session) {
		send(w, http.StatusConflict, map[string]any{"error": "These subtitles are pictures; they show once the episode has downloaded."})
		return
	}
	path := filepath.Join(session.Dir(), fmt.Sprintf("pgs-%d.sup", index))
	err = s.buildOnce(path, func(part string) error {
		cmd := proc.Background(exec.CommandContext(r.Context(), s.cfg.Tool("ffmpeg"), "-hide_banner", "-loglevel", "error",
			"-y", "-i", s.readable(r.Context(), session), "-map", fmt.Sprintf("0:%d", index), "-c:s", "copy", "-f", "sup", part))
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%v: %s", err, bytes.TrimSpace(out))
		}
		return nil
	})
	if err != nil {
		s.fail(w, "picture subtitles", err)
		return
	}
	w.Header().Set("X-Yoru-Complete", "true")
	serveFile(w, r, path, "application/octet-stream")
}

func (s *Server) holdWhileWatching(ctx context.Context, session *transcode.Session) bool {
	if s.sourceComplete(ctx, session) {
		return false
	}
	prefs, err := s.store.Prefs(ctx, 0)
	return err != nil || prefs.String("downloads.while_watching") != "continue"
}

func (s *Server) sourceComplete(ctx context.Context, session *transcode.Session) bool {
	if !isTorrentSource(session.Source) {
		return true
	}
	animeID, episode, ok := parseSessionID(session.ID)
	if !ok {
		return false
	}
	rec, found, err := s.store.TorrentForEpisode(ctx, animeID, strconv.Itoa(episode))
	if err != nil {
		return false
	}
	return !found || s.torrentWhole(ctx, rec)
}

func (s *Server) torrentWhole(ctx context.Context, rec store.TorrentRecord) bool {
	return s.store.FileComplete(ctx, rec.InfoHash, rec.FileIndex) || s.cache != nil && s.cache.Finished(ctx, rec.InfoHash)
}

func (s *Server) streamThumbnails(w http.ResponseWriter, r *http.Request) {
	session, ok := s.stream(w, r)
	if !ok {
		return
	}
	if s.thumbs == nil {
		send(w, http.StatusOK, transcode.Sheet{})
		return
	}

	ctx := r.Context()
	dir := s.thumbDir(ctx, session)
	duration, aspect := session.Info.Duration, aspectOf(session.Info)
	sheet, ready := s.thumbs.Read(dir, duration, aspect)
	local := s.readable(ctx, session)
	switch {
	case ready || isTorrentSource(local):
	case s.sourceComplete(ctx, session):
		s.thumbs.Generate(local, dir, duration, aspect)
	case !s.streams.Converting():
		if loaded, ok := s.loadedBytes(ctx, session); ok {
			have := make([]transcode.Span, len(loaded.Ranges))
			for i, r := range loaded.Ranges {
				have[i] = transcode.Span{From: r.From, To: r.To}
			}
			s.thumbs.Fill(local, dir, duration, aspect, have)
		}
	}
	send(w, http.StatusOK, sheet)
}

func (s *Server) loadedBytes(ctx context.Context, session *transcode.Session) (torrent.FileRanges, bool) {
	animeID, episode, ok := parseSessionID(session.ID)
	if !ok || s.cache == nil {
		return torrent.FileRanges{}, false
	}
	rec, found, err := s.store.TorrentForEpisode(ctx, animeID, strconv.Itoa(episode))
	if err != nil || !found {
		return torrent.FileRanges{}, false
	}
	return s.cache.Loaded(ctx, rec.InfoHash, rec.FileIndex)
}

func (s *Server) streamThumbSheet(w http.ResponseWriter, r *http.Request) {
	session, ok := s.stream(w, r)
	if !ok {
		return
	}
	path := transcode.SheetFile(s.thumbDir(r.Context(), session))
	if path == "" {
		http.NotFound(w, r)
		return
	}
	serveFile(w, r, path, "image/jpeg")
}

type StreamHealth struct {
	Mbps   float64 `json:"mbps"`
	Needed float64 `json:"needed"`
	Peers  int     `json:"peers"`

	Complete bool `json:"complete"`
	Slow     bool `json:"slow"`

	Speed   float64 `json:"speed,omitempty"`
	Percent float64 `json:"percent,omitempty"`
}

func (s *Server) streamHealth(w http.ResponseWriter, r *http.Request) {
	session, ok := s.stream(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	health := StreamHealth{Complete: s.sourceComplete(ctx, session), Speed: session.Speed()}
	if health.Complete {
		health.Percent = 100
		send(w, http.StatusOK, health)
		return
	}

	animeID, episode, ok := parseSessionID(session.ID)
	if !ok || s.cache == nil {
		send(w, http.StatusOK, health)
		return
	}
	rec, found, err := s.store.TorrentForEpisode(ctx, animeID, strconv.Itoa(episode))
	if err != nil || !found {
		send(w, http.StatusOK, health)
		return
	}
	if session.Info != nil && session.Info.Duration > 0 && rec.TotalSize > 0 {
		health.Needed = float64(rec.TotalSize) * 8 / session.Info.Duration / 1e6
	}
	health.Mbps, health.Peers = s.cache.Swarm(ctx, rec.InfoHash)
	if loaded, ok := s.cache.Loaded(ctx, rec.InfoHash, rec.FileIndex); ok && loaded.Length > 0 {
		var have int64
		for _, r := range loaded.Ranges {
			have += r.To - r.From
		}
		health.Percent = min(100, 100*float64(have)/float64(loaded.Length))
	}
	health.Slow = health.Needed > 0 && health.Mbps < health.Needed*1.2
	send(w, http.StatusOK, health)
}

func (s *Server) streamLoaded(w http.ResponseWriter, r *http.Request) {
	if s.streams == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "transcoder unavailable"})
		return
	}
	session, ok := s.streams.Peek(r.PathValue("id"))
	if !ok {
		send(w, http.StatusNotFound, map[string]any{"error": "no such stream session"})
		return
	}
	ctx := r.Context()
	duration := 0.0
	if session.Info != nil {
		duration = session.Info.Duration
	}
	if s.sourceComplete(ctx, session) {
		send(w, http.StatusOK, map[string]any{"complete": true, "ranges": [][2]float64{{0, duration}}})
		return
	}
	ranges := [][2]float64{}
	if loaded, ok := s.loadedBytes(ctx, session); ok {
		ranges = loadedSeconds(loaded, duration)
	}
	send(w, http.StatusOK, map[string]any{"complete": false, "ranges": ranges})
}

func loadedSeconds(loaded torrent.FileRanges, duration float64) [][2]float64 {
	out := [][2]float64{}
	if loaded.Length <= 0 || duration <= 0 {
		return out
	}
	for _, r := range loaded.Ranges {
		from := float64(r.From) / float64(loaded.Length) * duration
		to := float64(r.To) / float64(loaded.Length) * duration
		if to > from {
			out = append(out, [2]float64{from, to})
		}
	}
	return out
}

func (s *Server) thumbDir(ctx context.Context, session *transcode.Session) string {
	key := ""
	if animeID, episode, ok := parseSessionID(session.ID); ok && isTorrentSource(session.Source) {
		if rec, found, err := s.store.TorrentForEpisode(ctx, animeID, strconv.Itoa(episode)); err == nil && found {
			key = fmt.Sprintf("%s-%d", rec.InfoHash, rec.FileIndex)
		}
	}
	if key == "" {
		sum := sha1.Sum([]byte(session.Source))
		key = hex.EncodeToString(sum[:8])
	}
	dir := filepath.Join(s.cfg.CacheDir, "thumbs", key)
	os.MkdirAll(dir, 0o755)
	return dir
}

func aspectOf(info *transcode.MediaInfo) float64 {
	if info == nil || info.Video == nil || info.Video.Height == 0 {
		return 0
	}
	return float64(info.Video.Width) / float64(info.Video.Height)
}

func (s *Server) streamFont(w http.ResponseWriter, r *http.Request) {
	session, ok := s.stream(w, r)
	if !ok {
		return
	}

	name := r.PathValue("font")
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	serveFile(w, r, filepath.Join(session.Dir(), "fonts", name), "font/ttf")
}

func (s *Server) SessionIdle(id string) {
	s.downloader.Release(id)
	s.SessionGone(id)
	animeID, episode, ok := parseSessionID(id)
	if !ok || s.playback == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s.leftEpisode(ctx, id, animeID, episode)
}

func (s *Server) leftEpisode(ctx context.Context, id string, animeID, episode int) {
	if s.stillReading(ctx, animeID, episode) {
		return
	}
	if !s.downloader.Downloading(id) {
		s.playback.Suspend(ctx, animeID, episode)
		return
	}
	hash, index, err := s.store.CachedFile(ctx, animeID, strconv.Itoa(episode))
	if err != nil || hash == "" {
		return
	}
	if err := s.store.PinCache(ctx, hash, index, false); err != nil {
		s.log.Warn("unpin left episode", "anime", animeID, "episode", episode, "err", err)
	}
}

func (s *Server) SessionGone(id string) {
	if s.streams == nil {
		return
	}
	own := func(path string) bool { return s.streams.Owns(id, path) }
	s.fontsMu.Lock()
	maps.DeleteFunc(s.fontJobs, func(dir string, _ struct{}) bool { return own(dir) })
	maps.DeleteFunc(s.fonts, func(dir string, _ []transcode.FontFile) bool { return own(dir) })
	s.fontsMu.Unlock()
	if s.subtitles != nil {
		s.subtitles.ForgetOwned(own)
	}
	s.building.Range(func(path, mu any) bool {
		if own(path.(string)) && mu.(*sync.Mutex).TryLock() {
			s.building.Delete(path)
			mu.(*sync.Mutex).Unlock()
		}
		return true
	})
}

func (s *Server) streamClose(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	closed := s.streams != nil && s.streams.Close(id)

	if closed {
		s.downloader.Release(id)
		s.SessionGone(id)
	}
	if animeID, episode, ok := parseSessionID(id); closed && ok && s.playback != nil {
		s.leftEpisode(r.Context(), id, animeID, episode)
	}
	send(w, http.StatusOK, map[string]any{"closed": closed})
}

func (s *Server) stillReading(ctx context.Context, animeID, episode int) bool {
	if s.streams == nil {
		return false
	}

	rec, found, err := s.store.TorrentForEpisode(ctx, animeID, strconv.Itoa(episode))
	if err != nil || !found || rec.EngineID == 0 {
		return false
	}

	marker := fmt.Sprintf("/torrents/%d/", rec.EngineID)
	for _, source := range s.streams.Sources() {
		if strings.Contains(source, marker) {
			return true
		}
	}
	return false
}

func (s *Server) streamAudio(w http.ResponseWriter, r *http.Request) {
	if s.streams == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "transcoder unavailable"})
		return
	}
	session, ok := s.streams.Get(r.PathValue("id"))
	if !ok {
		send(w, http.StatusNotFound, map[string]any{"error": "no such stream"})
		return
	}
	track, err := strconv.Atoi(r.URL.Query().Get("track"))
	if err != nil || track < 0 || track >= len(session.Info.Audio) {
		send(w, http.StatusBadRequest, map[string]any{"error": "track is out of range"})
		return
	}
	changed := session.SetAudioTrack(track)
	send(w, http.StatusOK, map[string]any{"audioTrack": session.AudioTrack(), "changed": changed})
}

func soundFromQuery(r *http.Request, current sound.Options) (sound.Options, bool) {
	q := r.URL.Query()
	any := false
	if v := q.Get("voice"); v != "" {
		current.Voice, any = v, true
	}
	if v := q.Get("boost"); v != "" {
		current.Boost, any = v, true
	}
	for name, field := range map[string]*bool{"night": &current.Night, "headphones": &current.Headphones, "mono": &current.Mono} {
		if v := q.Get(name); v != "" {
			*field, any = v == "true" || v == "1", true
		}
	}
	return current, any
}

func (s *Server) streamSound(w http.ResponseWriter, r *http.Request) {
	session, ok := s.stream(w, r)
	if !ok {
		return
	}
	o, _ := soundFromQuery(r, session.Sound())
	if !sound.ValidVoice(o.Voice) || !sound.ValidBoost(o.Boost) {
		send(w, http.StatusBadRequest, map[string]any{"error": "voice and boost must be off, medium or high"})
		return
	}
	changed := session.SetSound(o)
	send(w, http.StatusOK, map[string]any{"sound": session.Sound(), "changed": changed})
}

func sessionID(animeID, episode int) string {
	return fmt.Sprintf("%d-%d", animeID, episode)
}

func isTorrentSource(src string) bool {
	return strings.Contains(src, "/torrents/") && strings.Contains(src, "/stream/")
}

func parseSessionID(id string) (animeID, episode int, ok bool) {
	id = transcode.BaseID(id)
	dash := strings.LastIndexByte(id, '-')
	if dash <= 0 {
		return 0, 0, false
	}
	animeID, err := strconv.Atoi(id[:dash])
	if err != nil {
		return 0, 0, false
	}
	episode, err = strconv.Atoi(id[dash+1:])
	if err != nil {
		return 0, 0, false
	}
	return animeID, episode, true
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request) (*transcode.Session, bool) {
	if s.streams == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "transcoder unavailable"})
		return nil, false
	}
	session, ok := s.streams.Get(r.PathValue("id"))
	if !ok {
		send(w, http.StatusNotFound, map[string]any{"error": "no such stream session"})
		return nil, false
	}
	return session, true
}

func serveFile(w http.ResponseWriter, r *http.Request, path, contentType string) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, path)
}
