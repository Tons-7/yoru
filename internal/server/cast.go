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
	if track := r.URL.Query().Get("burn"); track != "" {
		delay, _ := strconv.ParseFloat(r.URL.Query().Get("delay"), 64)
		burned, err := s.burnedSession(r.Context(), session, track, delay)
		switch {
		case errors.Is(err, errStillArriving):
			note = "Subtitles go as text this time: drawing them in needs the whole episode downloaded."
		case err != nil:
			s.log.Warn("cast with subtitles drawn in", "session", session.ID, "err", err)
			note = "Subtitles go as text: drawing them in failed."
		default:
			target = burned
		}
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

// burnedSession opens the episode with one subtitle track drawn in, shifted by delay seconds. The track must be
// whole: lines still to download would be missing from the picture for good.
func (s *Server) burnedSession(ctx context.Context, session *transcode.Session, track string, delay float64) (*transcode.Session, error) {
	if s.subtitles == nil {
		return nil, errors.New("subtitles unavailable")
	}
	if !s.sourceComplete(ctx, session) {
		return nil, errStillArriving
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	req.SetPathValue("id", session.ID)
	req.SetPathValue("track", track)
	rec := httptest.NewRecorder()
	s.streamSubtitle(rec, req)
	if rec.Code != http.StatusOK {
		return nil, fmt.Errorf("subtitle track %s: HTTP %d", track, rec.Code)
	}
	// Shifted by the player's delay and written as ASS, whatever the track was.
	file := filepath.Join(session.Dir(), fmt.Sprintf("cast-%s.ass", track))
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
	// Per track and delay: another choice is another picture.
	id := fmt.Sprintf("%s-burn%s-%d", session.ID, track, int(math.Round(delay*10)))
	return s.streams.OpenBurned(ctx, id, session.Source, local, transcode.Burn{Subtitles: file, Fonts: fonts})
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
