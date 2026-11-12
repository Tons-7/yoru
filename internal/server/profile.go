package server

import (
	"context"
	"encoding/base64"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"yoru/internal/anilist"
)

const (
	avatarSetting = "profile.avatar"
	nameSetting   = "profile.name"
	nameFrom      = "profile.name_from"
	avatarMax     = 32 << 10
	avatarKept    = 24 * time.Hour
	avatarRetry   = time.Hour
	avatarRecheck = 24 * time.Hour
	avatarFailed  = 10 * time.Minute
)

var avatarClient = &http.Client{
	Timeout: 15 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || !avatarHost(req.URL.String()) {
			return http.ErrUseLastResponse
		}
		return nil
	},
}

var avatarData = regexp.MustCompile(`^data:image/(webp|jpeg|png);base64,([A-Za-z0-9+/=]+)$`)

func parseAvatar(dataURL string) (kind string, body []byte, ok bool) {
	m := avatarData.FindStringSubmatch(dataURL)
	if m == nil {
		return "", nil, false
	}
	body, err := base64.StdEncoding.DecodeString(m[2])
	if err != nil || len(body) == 0 || len(body) > avatarMax {
		return "", nil, false
	}
	if kind = http.DetectContentType(body); kind != "image/"+m[1] {
		return "", nil, false
	}
	return kind, body, true
}

func versionOf(s string) string {
	h := fnv.New32a()
	io.WriteString(h, s)
	return fmt.Sprintf("%08x", h.Sum32())
}

type trackerAvatars struct {
	mu     sync.Mutex
	asked  map[string]time.Time
	url    string
	kind   string
	body   []byte
	loaded time.Time
	failed string
	failAt time.Time
}

func avatarHost(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return strings.HasSuffix(host, ".anilist.co") || strings.HasSuffix(host, ".myanimelist.net")
}

func (s *Server) trackerAvatarURL(ctx context.Context) (source, link string) {
	if token, _ := s.store.Setting(ctx, "anilist.token"); token != "" {
		link, _ = s.store.Setting(ctx, "anilist.avatar")
		if link == "" && s.avatars.due("anilist", avatarRetry) {
			if viewer, err := s.anilist.Viewer(ctx); err == nil && viewer.Avatar.Medium != "" {
				link = viewer.Avatar.Medium
				s.store.SetSetting(ctx, "anilist.avatar", link)
			}
		} else if link != "" && s.avatars.due("anilist", avatarRecheck) {
			go s.recheckAvatar("anilist.avatar", link, func(ctx context.Context) (string, error) {
				viewer, err := s.anilist.Viewer(anilist.Background(ctx))
				return viewer.Avatar.Medium, err
			})
		}
		if avatarHost(link) {
			return "anilist", link
		}
	}
	if s.mal != nil && s.mal.Authenticated() {
		link, _ = s.store.Setting(ctx, "mal.avatar")
		if link == "" && s.avatars.due("mal", avatarRetry) {
			if viewer, err := s.mal.Viewer(ctx); err == nil && viewer.Picture != "" {
				link = viewer.Picture
				s.store.SetSetting(ctx, "mal.avatar", link)
			}
		} else if link != "" && s.avatars.due("mal", avatarRecheck) {
			go s.recheckAvatar("mal.avatar", link, func(ctx context.Context) (string, error) {
				viewer, err := s.mal.Viewer(ctx)
				return viewer.Picture, err
			})
		}
		if avatarHost(link) {
			return "mal", link
		}
	}
	return "", ""
}

func (s *Server) recheckAvatar(setting, was string, ask func(context.Context) (string, error)) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	now, err := ask(ctx)
	if err != nil || now == "" || now == was {
		return
	}
	if still, _ := s.store.Setting(ctx, setting); still == was {
		s.store.SetSetting(ctx, setting, now)
	}
}

func (a *trackerAvatars) due(provider string, every time.Duration) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if at, ok := a.asked[provider]; ok && time.Since(at) < every {
		return false
	}
	if a.asked == nil {
		a.asked = map[string]time.Time{}
	}
	a.asked[provider] = time.Now()
	return true
}

func (a *trackerAvatars) fetch(ctx context.Context, link string) (string, []byte, error) {
	a.mu.Lock()
	if a.url == link && time.Since(a.loaded) < avatarKept {
		kind, body := a.kind, a.body
		a.mu.Unlock()
		return kind, body, nil
	}
	if a.failed == link && time.Since(a.failAt) < avatarFailed {
		a.mu.Unlock()
		return "", nil, fmt.Errorf("avatar: failed moments ago")
	}
	a.mu.Unlock()
	kind, body, err := a.load(ctx, link)
	a.mu.Lock()
	if err != nil {
		a.failed, a.failAt = link, time.Now()
	} else {
		a.url, a.kind, a.body, a.loaded = link, kind, body, time.Now()
	}
	a.mu.Unlock()
	return kind, body, err
}

func (a *trackerAvatars) load(ctx context.Context, link string) (string, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("User-Agent", "Yoru")
	res, err := avatarClient.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("avatar: HTTP %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		return "", nil, err
	}
	kind := http.DetectContentType(body)
	if !strings.HasPrefix(kind, "image/") {
		return "", nil, fmt.Errorf("avatar: not a picture (%s)", kind)
	}
	return kind, body, nil
}

func (s *Server) profileName(ctx context.Context) (own, from, shown string) {
	own, _ = s.store.Setting(ctx, nameSetting)
	from, _ = s.store.Setting(ctx, nameFrom)
	names := map[string]string{"own": own}
	names["anilist"], _ = s.store.Setting(ctx, "anilist.user_name")
	names["mal"], _ = s.store.Setting(ctx, "mal.user_name")
	if _, known := names[from]; !known {
		from = "anilist"
		if own != "" {
			from = "own"
		}
	}
	for _, source := range []string{from, "anilist", "mal", "own"} {
		if names[source] != "" {
			return own, from, names[source]
		}
	}
	return own, from, ""
}

func (s *Server) profile(w http.ResponseWriter, r *http.Request) {
	name, from, shown := s.profileName(r.Context())
	out := map[string]any{"name": name, "nameFrom": from, "shown": shown, "avatar": "", "version": ""}
	if own, _ := s.store.Setting(r.Context(), avatarSetting); own != "" {
		out["avatar"], out["version"] = "own", versionOf(own)
	} else if source, link := s.trackerAvatarURL(r.Context()); link != "" {
		out["avatar"], out["version"] = source, versionOf(link)
	}
	send(w, http.StatusOK, out)
}

func (s *Server) setProfileName(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name *string `json:"name"`
		From string  `json:"from"`
	}
	if !decode(w, r, &body) {
		return
	}
	values := map[string]string{}
	if body.Name != nil {
		values[nameSetting] = cleanName(*body.Name, "")
		values[nameFrom] = ""
		if values[nameSetting] != "" {
			values[nameFrom] = "own"
		}
	}
	switch body.From {
	case "anilist", "mal":
		values[nameFrom] = body.From
	case "own":
		own, typed := values[nameSetting]
		if !typed {
			own, _ = s.store.Setting(r.Context(), nameSetting)
		}
		values[nameFrom] = ""
		if own != "" {
			values[nameFrom] = "own"
		}
	case "":
	default:
		send(w, http.StatusBadRequest, map[string]any{"error": "unknown name source"})
		return
	}
	if err := s.store.SetSettings(r.Context(), values); err != nil {
		s.fail(w, "save name", err)
		return
	}
	s.profile(w, r)
}

func (s *Server) profileAvatar(w http.ResponseWriter, r *http.Request) {
	kind, body := "", []byte(nil)
	if own, _ := s.store.Setting(r.Context(), avatarSetting); own != "" {
		kind, body, _ = parseAvatar(own)
	} else if _, link := s.trackerAvatarURL(r.Context()); link != "" {
		var err error
		if kind, body, err = s.avatars.fetch(r.Context(), link); err != nil {
			s.log.Warn("tracker avatar", "err", err)
		}
	}
	if len(body) == 0 {
		send(w, http.StatusNotFound, map[string]any{"error": "no picture"})
		return
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Write(body)
}

func (s *Server) setProfileAvatar(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Image string `json:"image"`
	}
	if !decode(w, r, &body) {
		return
	}
	if _, _, ok := parseAvatar(body.Image); !ok {
		send(w, http.StatusBadRequest, map[string]any{"error": "That picture couldn't be used. Try a JPEG, PNG or WebP."})
		return
	}
	if err := s.store.SetSetting(r.Context(), avatarSetting, body.Image); err != nil {
		s.fail(w, "save avatar", err)
		return
	}
	s.profile(w, r)
}

func (s *Server) clearProfileAvatar(w http.ResponseWriter, r *http.Request) {
	if err := s.store.SetSetting(r.Context(), avatarSetting, ""); err != nil {
		s.fail(w, "remove avatar", err)
		return
	}
	s.profile(w, r)
}
