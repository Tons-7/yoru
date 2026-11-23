package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"mime"
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

const externalFor = 12 * time.Hour

type grant struct {
	Source  string `json:"s"`
	Expires int64  `json:"e"`
}

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
	link := requestOrigin(r) + "/x/" + s.signGrant(grant{Source: body.Source, Expires: expires.Unix()}) +
		"/" + url.PathEscape(name)
	send(w, http.StatusOK, map[string]any{"url": link, "name": name, "expires": expires.UTC().Format(time.RFC3339)})
}

func (s *Server) externalStream(w http.ResponseWriter, r *http.Request) {
	g, ok := s.openGrant(r.PathValue("grant"), time.Now())
	if !ok {
		http.Error(w, "this link has expired; open the episode in the app again from Yoru", http.StatusForbidden)
		return
	}
	defer s.serving()()
	save := func(h http.Header) {
		if r.URL.Query().Get("download") == "1" {
			h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": r.PathValue("name")}))
		}
	}
	if p, ok := s.localSource(r.Context(), g.Source); ok {
		f, err := os.Open(p)
		if err != nil {
			http.Error(w, "file unavailable", http.StatusNotFound)
			return
		}
		defer f.Close()
		save(w.Header())
		http.ServeContent(w, r, filepath.Base(p), time.Time{}, f)
		return
	}
	if !s.engineSource(g.Source) {
		http.Error(w, "file unavailable", http.StatusNotFound)
		return
	}
	target, _ := url.Parse(g.Source)
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL = target
			pr.Out.Host = target.Host
			for _, h := range []string{"Origin", "Referer", "Cookie", "Sec-Fetch-Site", "Sec-Fetch-Mode", "Sec-Fetch-Dest"} {
				pr.Out.Header.Del(h)
			}
		},
		ModifyResponse: func(res *http.Response) error {
			save(res.Header)
			return nil
		},
	}
	proxy.ServeHTTP(w, r)
}
