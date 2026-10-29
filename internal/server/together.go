package server

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"yoru/internal/config"
)

// Watch together: a room on the host's Yoru that friends with their own Yoru join by link. Each plays
// its own copy of the same release; only play, pause, seek, ready and chat pass through here.

const (
	togetherMax  = 8
	togetherFor  = 24 * time.Hour
	togetherIdle = 10 * time.Minute
)

var togetherReactions = []string{"😂", "😮", "❤️", "👏"}

type togetherState struct {
	Playing  bool    `json:"playing"`
	Position float64 `json:"position"`
	// At is when Position held, in server milliseconds, so a player can work out where the room is now.
	At       int64  `json:"at"`
	AnimeID  int    `json:"animeId"`
	Episode  int    `json:"episode"`
	InfoHash string `json:"infoHash,omitempty"`
	// Waiting: a play held until everyone is ready.
	Waiting bool `json:"waiting"`
}

type togetherMember struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Host      bool   `json:"host,omitempty"`
	Ready     bool   `json:"ready"`
	Connected bool   `json:"connected"`
	Admitted  bool   `json:"-"`
	token     string
	streams   int
	events    chan []byte
	// When they last went (or asked to join), for clearing out seats nobody comes back to.
	gone time.Time
}

type togetherRoom struct {
	mu      sync.Mutex
	code    string
	secret  string
	title   string
	created time.Time
	empty   time.Time
	ask     bool
	wait    bool
	state   togetherState
	members []*togetherMember
}

func nowMillis() int64 { return time.Now().UnixMilli() }

func (st togetherState) at(now int64) float64 {
	if !st.Playing {
		return st.Position
	}
	return st.Position + float64(now-st.At)/1000
}

func (r *togetherRoom) member(id, token string) *togetherMember {
	for _, m := range r.members {
		if m.ID == id && subtle.ConstantTimeCompare([]byte(m.token), []byte(token)) == 1 {
			return m
		}
	}
	return nil
}

func (r *togetherRoom) view() map[string]any {
	members := []*togetherMember{}
	for _, m := range r.members {
		if m.Admitted {
			members = append(members, m)
		}
	}
	return map[string]any{"type": "room", "code": r.code, "title": r.title, "ask": r.ask, "wait": r.wait,
		"state": r.state, "members": members, "now": nowMillis()}
}

// send goes to every admitted member, or only to those given.
func (r *togetherRoom) send(event map[string]any, to ...*togetherMember) {
	event["now"] = nowMillis()
	raw, _ := json.Marshal(event)
	targets := to
	if len(to) == 0 {
		for _, m := range r.members {
			if m.Admitted {
				targets = append(targets, m)
			}
		}
	}
	for _, m := range targets {
		select {
		case m.events <- raw:
		default:
		}
	}
}

func (r *togetherRoom) host() *togetherMember {
	for _, m := range r.members {
		if m.Host {
			return m
		}
	}
	return nil
}

// tidy clears seats left 2 minutes (a closed tab) and knocks left unanswered 10, and keeps a host: the
// longest here takes over, or nobody could let anyone in.
func (r *togetherRoom) tidy() {
	r.members = slices.DeleteFunc(r.members, func(m *togetherMember) bool {
		if m.Connected || m.gone.IsZero() {
			return false
		}
		wait := 2 * time.Minute
		if !m.Admitted {
			wait = 10 * time.Minute
		}
		return time.Since(m.gone) > wait
	})
	if r.host() != nil {
		return
	}
	for _, m := range r.members {
		if m.Admitted {
			m.Host = true
			return
		}
	}
}

// allReady: everyone here has the room's spot loaded; someone not connected doesn't hold the room.
func (r *togetherRoom) allReady() bool {
	for _, m := range r.members {
		if m.Admitted && m.Connected && !m.Ready {
			return false
		}
	}
	return true
}

func (r *togetherRoom) notReady() []string {
	var names []string
	for _, m := range r.members {
		if m.Admitted && m.Connected && !m.Ready {
			names = append(names, m.Name)
		}
	}
	return names
}

func (r *togetherRoom) play(force bool) {
	if r.wait && !force && !r.allReady() {
		r.state.Waiting = true
		return
	}
	r.state.Playing, r.state.Waiting, r.state.At = true, false, nowMillis()
}

func (s *Server) togetherRoom(w http.ResponseWriter, req *http.Request) *togetherRoom {
	s.togetherMu.Lock()
	s.expireRooms()
	room := s.rooms[req.PathValue("code")]
	s.togetherMu.Unlock()
	if room == nil || subtle.ConstantTimeCompare([]byte(room.secret), []byte(req.URL.Query().Get("k"))) != 1 {
		send(w, http.StatusNotFound, map[string]any{"error": "This room has ended, or the link is wrong."})
		return nil
	}
	return room
}

// togetherCORS lets a friend's page, served by their own Yoru, talk to this one: the link is the key.
func togetherCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
}

func (s *Server) togetherOptions(w http.ResponseWriter, _ *http.Request) {
	togetherCORS(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) createTogether(w http.ResponseWriter, req *http.Request) {
	var body struct {
		AnimeID  int     `json:"animeId"`
		Episode  int     `json:"episode"`
		InfoHash string  `json:"infoHash"`
		Title    string  `json:"title"`
		Position float64 `json:"position"`
		Playing  bool    `json:"playing"`
		Name     string  `json:"name"`
	}
	if !decode(w, req, &body) {
		return
	}
	base := ""
	if names := s.httpsNames(); len(names) > 0 {
		base = fmt.Sprintf("https://%s:%s", names[0], s.port())
	} else if config.Env("TOGETHER_LOCAL") != "" {
		// End-to-end checks only: a scratch instance has no HTTPS.
		base = requestOrigin(req)
	}
	if base == "" {
		send(w, http.StatusConflict, map[string]any{"error": "Friends reach the room over the internet: set up HTTPS " +
			"under Settings, Devices first.", "needs": "https"})
		return
	}
	if body.AnimeID == 0 || body.Episode <= 0 {
		send(w, http.StatusBadRequest, map[string]any{"error": "Start the episode first."})
		return
	}
	if body.InfoHash == "" {
		send(w, http.StatusBadRequest, map[string]any{"error": "Watch together needs a torrent release, so friends can fetch the same one; this is a file of yours."})
		return
	}
	room := &togetherRoom{
		code: strings.ToLower(newPairCode()), secret: NewAccessToken(), title: body.Title, created: time.Now(), wait: true,
		state: togetherState{Playing: body.Playing, Position: body.Position, At: nowMillis(), AnimeID: body.AnimeID,
			Episode: body.Episode, InfoHash: strings.ToLower(body.InfoHash)},
	}
	host := &togetherMember{ID: randomHex()[:12], Name: cleanName(body.Name, "Host"), Host: true, Admitted: true,
		token: NewAccessToken(), events: make(chan []byte, 256)}
	room.members = []*togetherMember{host}
	s.togetherMu.Lock()
	s.expireRooms()
	s.rooms[room.code] = room
	s.togetherMu.Unlock()
	link := fmt.Sprintf("%s/together/%s?k=%s", base, room.code, room.secret)
	s.log.Info("watch together room opened", "room", room.code, "anime", body.AnimeID, "episode", body.Episode)
	send(w, http.StatusOK, map[string]any{"code": room.code, "link": link, "member": host.ID, "token": host.token})
}

// expireRooms drops rooms a day old, or left empty a while; s.togetherMu is held.
func (s *Server) expireRooms() {
	for code, room := range s.rooms {
		room.mu.Lock()
		gone := time.Since(room.created) > togetherFor || !room.empty.IsZero() && time.Since(room.empty) > togetherIdle
		room.mu.Unlock()
		if gone {
			delete(s.rooms, code)
		}
	}
}

func cleanName(name, fallback string) string {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 32 {
			return -1
		}
		return r
	}, name))
	if utf8.RuneCountInString(name) > 32 {
		name = string([]rune(name)[:32])
	}
	if name == "" {
		return fallback
	}
	return name
}

// uniqueName numbers a name already in the room, Friend then Friend 2, so the feed tells people apart.
func uniqueName(members []*togetherMember, name string) string {
	taken := func(n string) bool {
		return slices.ContainsFunc(members, func(m *togetherMember) bool { return strings.EqualFold(m.Name, n) })
	}
	if !taken(name) {
		return name
	}
	for i := 2; ; i++ {
		if n := fmt.Sprintf("%s %d", name, i); !taken(n) {
			return n
		}
	}
}

func (s *Server) togetherInfo(w http.ResponseWriter, req *http.Request) {
	togetherCORS(w)
	room := s.togetherRoom(w, req)
	if room == nil {
		return
	}
	room.mu.Lock()
	defer room.mu.Unlock()
	send(w, http.StatusOK, room.view())
}

func (s *Server) joinTogether(w http.ResponseWriter, req *http.Request) {
	togetherCORS(w)
	room := s.togetherRoom(w, req)
	if room == nil {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if !decode(w, req, &body) {
		return
	}
	room.mu.Lock()
	defer room.mu.Unlock()
	room.tidy()
	if len(room.members) >= togetherMax {
		send(w, http.StatusConflict, map[string]any{"error": fmt.Sprintf("The room is full (%d people).", togetherMax)})
		return
	}
	m := &togetherMember{ID: randomHex()[:12], Name: uniqueName(room.members, cleanName(body.Name, "Friend")), Admitted: !room.ask,
		token: NewAccessToken(), events: make(chan []byte, 256), gone: time.Now()}
	room.members = append(room.members, m)
	if room.ask {
		if host := room.host(); host != nil {
			room.send(map[string]any{"type": "knock", "member": m.ID, "name": m.Name}, host)
		}
	}
	send(w, http.StatusOK, map[string]any{"member": m.ID, "token": m.token, "admitted": m.Admitted})
}

func (s *Server) togetherEvents(w http.ResponseWriter, req *http.Request) {
	togetherCORS(w)
	room := s.togetherRoom(w, req)
	if room == nil {
		return
	}
	q := req.URL.Query()
	room.mu.Lock()
	m := room.member(q.Get("m"), q.Get("t"))
	if m == nil {
		room.mu.Unlock()
		send(w, http.StatusForbidden, map[string]any{"error": "Not in this room."})
		return
	}
	m.streams++
	m.Connected, room.empty = true, time.Time{}
	if m.Admitted {
		room.send(room.view())
	}
	room.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	flusher, _ := w.(http.Flusher)
	write := func(raw []byte) bool {
		if _, err := fmt.Fprintf(w, "data: %s\n\n", raw); err != nil {
			return false
		}
		if flusher != nil {
			flusher.Flush()
		}
		return true
	}
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	defer func() {
		room.mu.Lock()
		if m.streams--; m.streams == 0 {
			m.Connected, m.Ready, m.gone = false, false, time.Now()
			// Someone gone mustn't keep the room waiting.
			if room.state.Waiting && room.allReady() {
				room.play(false)
				room.send(map[string]any{"type": "state", "state": room.state})
			}
			room.send(room.view())
			if !slices.ContainsFunc(room.members, func(o *togetherMember) bool { return o.Connected }) {
				room.empty = time.Now()
			}
		}
		room.mu.Unlock()
	}()
	if !m.Admitted {
		write([]byte(`{"type":"pending"}`))
	}
	for {
		select {
		case <-req.Context().Done():
			return
		case raw := <-m.events:
			if !write(raw) {
				return
			}
		case <-tick.C:
			if !write(fmt.Appendf(nil, `{"type":"tick","now":%d}`, nowMillis())) {
				return
			}
		}
	}
}

func (s *Server) togetherAct(w http.ResponseWriter, req *http.Request) {
	togetherCORS(w)
	room := s.togetherRoom(w, req)
	if room == nil {
		return
	}
	var body struct {
		Member   string  `json:"member"`
		Token    string  `json:"token"`
		Type     string  `json:"type"`
		Position float64 `json:"position"`
		Ready    bool    `json:"ready"`
		AnimeID  int     `json:"animeId"`
		Episode  int     `json:"episode"`
		InfoHash string  `json:"infoHash"`
		Text     string  `json:"text"`
		// Host only.
		Ask   *bool  `json:"ask"`
		Wait  *bool  `json:"wait"`
		Admit string `json:"admit"`
		Allow bool   `json:"allow"`
	}
	if !decode(w, req, &body) {
		return
	}
	room.mu.Lock()
	defer room.mu.Unlock()
	m := room.member(body.Member, body.Token)
	if m == nil || !m.Admitted {
		send(w, http.StatusForbidden, map[string]any{"error": "Not in this room."})
		return
	}
	now := nowMillis()
	st := &room.state
	state := func(reason string) {
		room.send(map[string]any{"type": "state", "state": *st, "by": m.Name, "reason": reason, "waitingFor": room.notReady()})
	}
	switch body.Type {
	case "play":
		st.Position, st.At = st.at(now), now
		room.play(false)
		state("play")
	case "start":
		if !m.Host {
			send(w, http.StatusForbidden, map[string]any{"error": "Only the room's host can start without everyone."})
			return
		}
		room.play(true)
		state("play")
	case "pause":
		st.Playing, st.Waiting, st.Position, st.At = false, false, max(0, body.Position), now
		state("pause")
	case "seek":
		resume := st.Playing || st.Waiting
		st.Position, st.At, st.Playing = max(0, body.Position), now, false
		for _, o := range room.members {
			o.Ready = false
		}
		st.Waiting = false
		if resume {
			room.play(false)
		}
		state("seek")
	case "ready":
		m.Ready = body.Ready
		switch {
		case st.Waiting && room.allReady():
			room.play(false)
			state("ready")
		case !body.Ready && st.Playing && room.wait:
			// Someone's connection can't keep up: the room waits for them.
			st.Position, st.At, st.Playing, st.Waiting = st.at(now), now, false, true
			state("buffering")
		default:
			room.send(room.view())
		}
	case "episode":
		if body.AnimeID == 0 || body.Episode <= 0 || body.InfoHash == "" {
			send(w, http.StatusBadRequest, map[string]any{"error": "Which episode?"})
			return
		}
		*st = togetherState{AnimeID: body.AnimeID, Episode: body.Episode, InfoHash: strings.ToLower(body.InfoHash), At: now}
		for _, o := range room.members {
			o.Ready = false
		}
		room.play(false)
		state("episode")
	case "chat", "react":
		text := strings.TrimSpace(body.Text)
		if body.Type == "react" && !slices.Contains(togetherReactions, text) || text == "" {
			send(w, http.StatusBadRequest, map[string]any{"error": "Nothing to send."})
			return
		}
		if utf8.RuneCountInString(text) > 500 {
			text = string([]rune(text)[:500])
		}
		room.send(map[string]any{"type": body.Type, "from": m.Name, "member": m.ID, "text": text})
	case "settings", "admit":
		if !m.Host {
			send(w, http.StatusForbidden, map[string]any{"error": "Only the room's host can change that."})
			return
		}
		if body.Ask != nil {
			room.ask = *body.Ask
		}
		if body.Wait != nil {
			room.wait = *body.Wait
		}
		if body.Admit != "" {
			for i, o := range room.members {
				if o.ID != body.Admit || o.Admitted {
					continue
				}
				if body.Allow {
					o.Admitted = true
					room.send(map[string]any{"type": "admitted"}, o)
				} else {
					room.send(map[string]any{"type": "refused"}, o)
					room.members = slices.Delete(room.members, i, i+1)
				}
				break
			}
		}
		room.send(room.view())
	case "leave":
		room.members = slices.DeleteFunc(room.members, func(o *togetherMember) bool { return o == m })
		room.tidy()
		room.send(room.view())
	default:
		send(w, http.StatusBadRequest, map[string]any{"error": "Unknown action."})
		return
	}
	send(w, http.StatusOK, map[string]any{"ok": true})
}

// togetherPage: a friend opening the link in a browser is told to paste it into their own Yoru.
func (s *Server) togetherPage(w http.ResponseWriter, req *http.Request) {
	link := "https://" + req.Host + req.URL.RequestURI()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1"><meta name="theme-color" content="#0b0d12">
<title>Watch together on Yoru</title><style>
body{margin:0;min-height:100vh;display:grid;place-items:center;background:#0b0d12;color:#d6dbe6;font:18px/1.5 system-ui,sans-serif}
main{max-width:32rem;padding:2rem;text-align:center}h1{font-size:1.5rem;margin:0 0 .75rem}p{margin:0 0 1rem;color:#8b93a7}
code{display:block;margin:1rem 0;padding:.75rem;border-radius:.6rem;background:#161a23;color:#fff;font-size:.8rem;word-break:break-all}
button{font:inherit;font-weight:600;padding:.6rem 1.2rem;border:0;border-radius:.6rem;background:#6f5cff;color:#fff;cursor:pointer}
</style></head><body><main><h1>Watch together on Yoru</h1>
<p>Open your own Yoru and paste this link into its search. It fetches the episode itself and joins the room.</p>
<code id="link">`+html.EscapeString(link)+`</code>
<button onclick="navigator.clipboard.writeText(document.getElementById('link').textContent).then(()=>this.textContent='Copied')">Copy link</button>
</main></body></html>`)
}
