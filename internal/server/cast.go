package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"yoru/internal/proc"
	"yoru/internal/transcode"
)

// A Chromecast plays the episode from Yoru itself: a link signed for one stream session (the TV can't carry the
// browser's cookie) opens its playlist, segments and subtitles, nothing else. Links last as long as an app's.

const castPrefix = "cast:"

// castLink makes the TV's address for a session: the HTTPS name when there is one (the receiver is an HTTPS
// page, and a phone's own address may be localhost), else the address this request came in on.
func (s *Server) castLink(w http.ResponseWriter, r *http.Request) {
	session, ok := s.stream(w, r)
	if !ok {
		return
	}
	// Bound to this machine alone, Yoru is out of the TV's reach.
	if s.rebind != nil && !s.bound() {
		send(w, http.StatusConflict, map[string]any{
			"error": "the TV can't reach Yoru. Turn on access from other devices under Settings, Devices.",
		})
		return
	}
	// ?burn=<track>: that track drawn into the picture, shifted by ?delay=, in a session of its own.
	target, note := session, ""
	animeID, _, _ := parseSessionID(session.ID)
	want := s.streamWant(r.Context(), animeID, "")
	if raw := r.URL.Query().Get("burn"); raw != "" {
		// A number, as a track is: the rest of its way names files on disk.
		track, err := strconv.Atoi(raw)
		if err != nil || track < 0 {
			send(w, http.StatusBadRequest, map[string]any{"error": "invalid subtitle track"})
			return
		}
		delay, _ := strconv.ParseFloat(r.URL.Query().Get("delay"), 64)
		burned, err := s.burnedSession(r.Context(), session, track, delay, want)
		switch {
		case errors.Is(err, errStillArriving):
			note = "Subtitles go as text this time: drawing them in needs the whole episode downloaded."
		case err != nil:
			s.log.Warn("cast with subtitles drawn in", "session", session.ID, "err", err)
			note = "Subtitles go as text: drawing them in failed."
		default:
			s.heldOnce(burned)
			target = burned
		}
	}
	// Unless burned in, the TV gets an any-browser session: it may not decode this copy (HEVC) or want its cap.
	if plan := session.CurrentPlan(); target == session && (plan.ForDevice || plan.MaxRate > 0) {
		// The episode's own any-browser session when one plays alike, rather than a third pass of the same plan;
		// else one of the TV's own, as its track and sound must not change another device's.
		id := transcode.BaseID(session.ID) + "-cast"
		if base, ok := s.streams.Get(transcode.BaseID(session.ID)); ok && want == (transcode.Want{}) &&
			base.Source == session.Source && base.AudioTrack() == session.AudioTrack() && base.Sound() == session.Sound() {
			id = base.ID
		}
		tv, err := s.streams.OpenFor(r.Context(), id, session.Source, s.readable(r.Context(), session), nil, want)
		if err != nil {
			s.fail(w, "open stream for the TV", err)
			return
		}
		s.heldOnce(tv)
		if tv.ID != transcode.BaseID(session.ID) {
			tv.SetAudioTrack(session.AudioTrack())
			tv.SetSound(session.Sound())
		}
		session, target = tv, tv
	}

	base := requestOrigin(r)
	if names := s.httpsNames(); len(names) > 0 && r.TLS == nil {
		base = fmt.Sprintf("https://%s:%s", names[0], s.port())
	} else if lan := lanAddrs(); loopbackHost(r.Host) && len(lan) > 0 {
		// The host's own window is on loopback, which on the TV is the TV itself.
		base = fmt.Sprintf("http://%s:%s", lan[0], s.port())
	}
	expires := time.Now().Add(externalFor)
	root := base + "/c/" + s.signGrant(grant{Source: castPrefix + target.ID, Expires: expires.Unix()}) + "/"

	type track struct {
		Index    int    `json:"index"`
		Language string `json:"language,omitempty"`
		Title    string `json:"title,omitempty"`
		URL      string `json:"url"`
	}
	tracks := []track{}
	for _, sub := range session.Info.Subtitles {
		// Drawn in, the picture carries them; a text track as well would show them twice.
		if target != session || !transcode.TextSubtitle(sub.Codec) {
			continue
		}
		tracks = append(tracks, track{Index: sub.Index, Language: sub.Language, Title: sub.Title,
			URL: root + "sub-" + strconv.Itoa(sub.Index) + ".vtt"})
	}
	send(w, http.StatusOK, map[string]any{
		"url": root + "playlist.m3u8", "subtitles": tracks, "burned": target != session, "note": note,
	})
}

var errStillArriving = errors.New("the episode is still downloading")

// heldOnce keeps one hold on a TV's session however often Cast is pressed; nothing closes it but the reaper.
func (s *Server) heldOnce(tv *transcode.Session) {
	if tv.Holders() > 1 {
		s.streams.Close(tv.ID)
	}
}

// burnedSession opens the episode with one subtitle track drawn in, shifted by delay seconds. The track must be
// whole: lines still to download would be missing from the picture for good.
func (s *Server) burnedSession(ctx context.Context, session *transcode.Session, track int, delay float64, want transcode.Want) (*transcode.Session, error) {
	// Per track, delay and softening: another choice is another picture.
	id := fmt.Sprintf("%s-burn%d-%d", transcode.BaseID(session.ID), track, int(math.Round(delay*10)))
	if want.Soften {
		id += "-soft"
	}
	// Pictures are laid over from the episode itself as it is read, each arriving with its stretch of picture.
	if transcode.PictureSubtitle(session.Info.SubtitleCodec(track)) {
		return s.streams.OpenBurned(ctx, id, session.Source, s.readable(ctx, session), transcode.Burn{Pictures: track, Delay: delay}, want)
	}
	if s.subtitles == nil {
		return nil, errors.New("subtitles unavailable")
	}
	if !s.sourceComplete(ctx, session) {
		return nil, errStillArriving
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	req.SetPathValue("id", session.ID)
	req.SetPathValue("track", strconv.Itoa(track))
	rec := httptest.NewRecorder()
	s.streamSubtitle(rec, req)
	if rec.Code != http.StatusOK {
		return nil, fmt.Errorf("subtitle track %d: HTTP %d", track, rec.Code)
	}
	// Shifted by the player's delay and written as ASS, whatever the track was.
	file := filepath.Join(session.Dir(), fmt.Sprintf("cast-%d.ass", track))
	cmd := proc.Hide(exec.CommandContext(ctx, s.cfg.Tool("ffmpeg"), "-hide_banner", "-loglevel", "error", "-y",
		"-itsoffset", fmt.Sprintf("%.3f", delay), "-i", "pipe:0", "-c:s", "ass", file))
	cmd.Stdin = bytes.NewReader(rec.Body.Bytes())
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("subtitles: %v: %s", err, bytes.TrimSpace(out))
	}
	local := s.readable(ctx, session)
	fonts := ""
	if s.waitFonts(ctx, session) {
		fonts = filepath.Join(session.Dir(), "fonts")
	}
	return s.streams.OpenBurned(ctx, id, session.Source, local, transcode.Burn{Subtitles: file, Fonts: fonts}, want)
}

// waitFonts waits for the session's fonts from the one extraction its folder runs at a time (the browser's
// may be under way; two would clear each other's files), starting it if none has run.
func (s *Server) waitFonts(ctx context.Context, session *transcode.Session) bool {
	started := false
	for {
		s.fontsMu.Lock()
		_, done := s.fonts[session.Dir()]
		_, running := s.fontJobs[session.Dir()]
		s.fontsMu.Unlock()
		switch {
		case done:
			return true
		case !running && started:
			return false
		case !running:
			s.extractFontsAsync(session, "/api/stream/"+session.ID)
			started = true
			continue
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// castFile serves the TV what a link opens, through the browser player's own handlers.
func (s *Server) castFile(w http.ResponseWriter, r *http.Request) {
	// The receiver is another origin reading without credentials: any origin may, the link is the key.
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Range")
	w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	g, ok := s.openGrant(r.PathValue("grant"), time.Now())
	if !ok || !strings.HasPrefix(g.Source, castPrefix) {
		http.Error(w, "this cast link has expired; cast again from Yoru", http.StatusForbidden)
		return
	}
	defer s.serving()()
	r.SetPathValue("id", strings.TrimPrefix(g.Source, castPrefix))
	file := r.PathValue("file")
	switch {
	case file == "playlist.m3u8":
		s.streamPlaylist(w, r)
	case file == "init.mp4":
		s.streamInit(w, r)
	case strings.HasPrefix(file, "sub-") && strings.HasSuffix(file, ".vtt"):
		r.SetPathValue("track", strings.TrimSuffix(strings.TrimPrefix(file, "sub-"), ".vtt"))
		s.castSubtitle(w, r)
	case strings.HasSuffix(file, ".mp4"):
		r.SetPathValue("segment", file)
		s.streamSegment(w, r)
	default:
		http.NotFound(w, r)
	}
}

// castSubtitle is the track as WebVTT, the one text format the receiver draws: styling and positions go.
func (s *Server) castSubtitle(w http.ResponseWriter, r *http.Request) {
	rec := httptest.NewRecorder()
	s.streamSubtitle(rec, r)
	if rec.Code != http.StatusOK {
		http.Error(w, "subtitles unavailable", rec.Code)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	cmd := proc.Hide(exec.CommandContext(ctx, s.cfg.Tool("ffmpeg"), "-hide_banner", "-loglevel", "error",
		"-i", "pipe:0", "-f", "webvtt", "pipe:1"))
	cmd.Stdin = bytes.NewReader(rec.Body.Bytes())
	vtt, err := cmd.Output()
	if err != nil {
		s.log.Warn("cast subtitles", "err", err)
		http.Error(w, "subtitles could not be converted", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(vtt)
}
