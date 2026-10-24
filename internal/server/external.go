package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Player apps (VLC, mpv, Outplayer) get links of their own: they can't carry the browser's cookie, and the
// access token kept in their history would open all of Yoru. A link reads one file, for externalFor.

const externalFor = 12 * time.Hour

type grant struct {
	Source  string `json:"s"`
	Expires int64  `json:"e"`
}

// grantKey changes with the access token, so resetting pairing ends every link; grantSecret, with a restart;
// grantEpoch, when a device is denied or removed (a link doesn't say which device made it).
func (s *Server) grantKey() []byte {
	m := hmac.New(sha256.New, s.grantSecret)
	m.Write([]byte(s.accessToken()))
	m.Write([]byte(strconv.FormatInt(s.grantEpoch.Load(), 10)))
	return m.Sum(nil)
}

func (s *Server) signGrant(g grant) string {
	payload, _ := json.Marshal(g)
	body := base64.RawURLEncoding.EncodeToString(payload)
	m := hmac.New(sha256.New, s.grantKey())
	m.Write([]byte(body))
	return body + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (s *Server) openGrant(token string, now time.Time) (grant, bool) {
	body, sig, ok := strings.Cut(token, ".")
	if !ok {
		return grant{}, false
	}
	m := hmac.New(sha256.New, s.grantKey())
	m.Write([]byte(body))
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, m.Sum(nil)) {
		return grant{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(body)
	var g grant
	if err != nil || json.Unmarshal(payload, &g) != nil || now.Unix() >= g.Expires {
		return grant{}, false
	}
	return g, true
}

var engineFileRoute = regexp.MustCompile(`^/torrents/(\d+)/stream/(\d+)$`)

// externalLink makes a player app's link to the file the player streams, at the address this device used.
func (s *Server) externalLink(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source string `json:"source"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		send(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	var name string
	if p, ok := s.localSource(r.Context(), body.Source); ok {
		name = filepath.Base(p)
	} else if s.engineSource(body.Source) && s.engine != nil {
		u, _ := url.Parse(body.Source)
		m := engineFileRoute.FindStringSubmatch(u.Path)
		if m == nil {
			send(w, http.StatusBadRequest, map[string]any{"error": "unrecognised source"})
			return
		}
		id, _ := strconv.Atoi(m[1])
		file, _ := strconv.Atoi(m[2])
		d, err := s.engine.Details(r.Context(), id)
		if err != nil || file >= len(d.Files) {
			send(w, http.StatusNotFound, map[string]any{"error": "that download is gone"})
			return
		}
		name = path.Base(strings.ReplaceAll(d.Files[file].Name, `\`, "/"))
	} else {
		send(w, http.StatusBadRequest, map[string]any{"error": "unrecognised source"})
		return
	}

	expires := time.Now().Add(externalFor)
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	link := scheme + "://" + r.Host + "/x/" + s.signGrant(grant{Source: body.Source, Expires: expires.Unix()}) +
		"/" + url.PathEscape(name)
	send(w, http.StatusOK, map[string]any{"url": link, "name": name, "expires": expires.UTC().Format(time.RFC3339)})
}

// externalStream serves a link's file, ranged, to whatever app holds the link; the guard lets it through.
func (s *Server) externalStream(w http.ResponseWriter, r *http.Request) {
	g, ok := s.openGrant(r.PathValue("grant"), time.Now())
	if !ok {
		http.Error(w, "this link has expired; open the episode in the app again from Yoru", http.StatusForbidden)
		return
	}
	defer s.serving()()
	if p, ok := s.localSource(r.Context(), g.Source); ok {
		f, err := os.Open(p)
		if err != nil {
			http.Error(w, "file unavailable", http.StatusNotFound)
			return
		}
		defer f.Close()
		http.ServeContent(w, r, filepath.Base(p), time.Time{}, f)
		return
	}
	if !s.engineSource(g.Source) {
		http.Error(w, "file unavailable", http.StatusNotFound)
		return
	}
	target, _ := url.Parse(g.Source)
	proxy := &httputil.ReverseProxy{Rewrite: func(pr *httputil.ProxyRequest) {
		pr.Out.URL = target
		pr.Out.Host = target.Host
		// The engine turns away anything that looks like a web page.
		for _, h := range []string{"Origin", "Referer", "Cookie", "Sec-Fetch-Site", "Sec-Fetch-Mode", "Sec-Fetch-Dest"} {
			pr.Out.Header.Del(h)
		}
	}}
	proxy.ServeHTTP(w, r)
}
