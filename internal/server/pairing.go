package server

import (
	"crypto/rand"
	"html"
	"maps"
	"net/http"
	"strings"
	"time"

	"yoru/internal/store"
)

// Pairing with a code: a TV without the token is shown a short code, not a refusal; typed under Settings,
// Devices on a device already in, it gives the TV the token, so nobody types a long link with a remote.

const (
	pairCookie = "yoru_pair"
	pairFor    = 10 * time.Minute
	// maxPairing: enough for a household; past it a flood of pages can't keep a real one waiting for long.
	maxPairing = 20
	// pairPerAddr: a scanner or link preview keeping no cookies takes its own few places, not everyone's.
	pairPerAddr = 3
	// pairAlphabet has nothing to mistake across a room: no 0 and O, no 1 and I.
	pairAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
)

type pairing struct {
	code     string
	name     string
	addr     string
	created  time.Time
	approved bool
}

// newPairCode is six letters and digits from pairAlphabet, shown as "K7Q-2MX".
func newPairCode() string {
	b := make([]byte, 6)
	rand.Read(b)
	for i := range b {
		b[i] = pairAlphabet[int(b[i])%len(pairAlphabet)]
	}
	return string(b)
}

// normalPairCode is what was typed, without its dash or spaces and in capitals, so "k7q 2mx" finds "K7Q2MX".
func normalPairCode(typed string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' {
			return -1
		}
		return r
	}, strings.ToUpper(strings.TrimSpace(typed)))
}

// expirePairs drops codes past pairFor; s.pairMu is held.
func (s *Server) expirePairs(now time.Time) {
	maps.DeleteFunc(s.pairs, func(_ string, p *pairing) bool { return now.Sub(p.created) > pairFor })
}

// pairOf is the pairing a request's cookie names, unexpired; s.pairMu is held.
func (s *Server) pairOf(r *http.Request, now time.Time) (string, *pairing) {
	s.expirePairs(now)
	c, err := r.Cookie(pairCookie)
	if err != nil {
		return "", nil
	}
	return c.Value, s.pairs[c.Value]
}

// pairPage shows a device without the token the code to pair it with, the same one again on a reload.
func (s *Server) pairPage(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	s.pairMu.Lock()
	_, p := s.pairOf(r, now)
	addr := clientAddr(r)
	from := 0
	for _, other := range s.pairs {
		if other.addr == addr {
			from++
		}
	}
	if p == nil && len(s.pairs) < maxPairing && from < pairPerAddr {
		secret := NewAccessToken()
		p = &pairing{code: newPairCode(), name: deviceName(r.UserAgent()), addr: addr, created: now}
		s.pairs[secret] = p
		keepCookie(w, r, pairCookie, secret)
		s.log.Info("device asks to pair with a code", "device", p.name, "addr", addr)
	}
	code, name := "", ""
	if p != nil {
		code, name = p.code, p.name
	}
	s.pairMu.Unlock()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(pairingPage(code, name)))
}

// pairWait is the pairing page asking "accepted yet?"; once so, the device gets the token (and with device
// approval on, an accepted identity), and the page opens Yoru.
func (s *Server) pairWait(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	s.pairMu.Lock()
	secret, p := s.pairOf(r, now)
	approved := p != nil && p.approved
	name := ""
	if approved {
		name = p.name
		delete(s.pairs, secret)
	}
	s.pairMu.Unlock()
	switch {
	case p == nil:
		send(w, http.StatusOK, map[string]any{"status": "expired"})
	case !approved:
		send(w, http.StatusOK, map[string]any{"status": "waiting"})
	default:
		keepCookie(w, r, accessCookie, s.accessToken())
		s.acceptPaired(w, r, name)
		send(w, http.StatusOK, map[string]any{"status": "approved"})
	}
}

// acceptPaired gives a paired device an accepted identity when device approval is on: the code was the host's
// say, so it isn't asked again.
func (s *Server) acceptPaired(w http.ResponseWriter, r *http.Request, name string) {
	a := &s.approvals
	a.mu.Lock()
	defer a.mu.Unlock()
	s.loadApprovals(r.Context())
	if a.mode == approvalOff {
		return
	}
	secret := NewAccessToken()
	id := deviceID(secret)
	if err := s.store.RequestAccess(r.Context(), id, name, clientAddr(r)); err != nil {
		s.log.Warn("record a paired device", "err", err)
		return
	}
	if err := s.store.DecideDevice(r.Context(), id, store.DeviceApproved); err != nil {
		s.log.Warn("accept a paired device", "err", err)
		return
	}
	keepCookie(w, r, deviceCookie, secret)
	a.status[id], a.seen[id] = store.DeviceApproved, time.Now()
}

// pairApprove takes the code a TV shows. With device approval on only the host may, as the host decides.
func (s *Server) pairApprove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &body) {
		return
	}
	a := &s.approvals
	a.mu.Lock()
	s.loadApprovals(r.Context())
	gated := a.mode != approvalOff
	a.mu.Unlock()
	if gated && !fromHost(r) {
		send(w, http.StatusForbidden, map[string]any{"error": "With device approval on, pair from the computer running Yoru."})
		return
	}
	code := normalPairCode(body.Code)
	now := time.Now()
	s.pairMu.Lock()
	defer s.pairMu.Unlock()
	s.expirePairs(now)
	for _, p := range s.pairs {
		if p.code == code && !p.approved {
			p.approved = true
			s.log.Info("device paired with a code", "device", p.name)
			send(w, http.StatusOK, map[string]any{"paired": p.name})
			return
		}
	}
	send(w, http.StatusNotFound, map[string]any{"error": "No device is showing that code. Codes last 10 minutes; reload the TV's page for a new one."})
}

// pairingPage is what a device without the token sees: self-contained, as the app stays behind the token.
func pairingPage(code, name string) string {
	shown := `<p>Too many devices are waiting to pair. Try again in a few minutes.</p>`
	if code != "" {
		shown = `<p>On a phone or computer already using Yoru, open Settings, then Devices, and enter this code under
Pair a device:</p><div class="code">` + code[:3] + "-" + code[3:] + `</div><p class="small">` + html.EscapeString(name) +
			` · the code lasts 10 minutes. This page opens Yoru as soon as it's entered.</p>`
	}
	return `<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1"><meta name="theme-color" content="#0b0c10">
<title>Pair with Yoru</title><style>
body{margin:0;min-height:100vh;display:grid;place-items:center;background:#0b0c10;color:#d6dbe6;font:18px/1.5 system-ui,sans-serif}
main{max-width:30rem;padding:2rem;text-align:center}h1{font-size:1.5rem;margin:0 0 .75rem}p{margin:0;color:#8b93a7}
.code{margin:1.5rem 0;font:600 3.5rem/1 ui-monospace,monospace;letter-spacing:.15em;color:#fff}.small{font-size:.9rem}
</style></head><body><main><h1>Pair this device</h1>` + shown + `</main>
<script>setInterval(async()=>{try{const r=await fetch('/api/pair/wait',{cache:'no-store'});const s=(await r.json()).status;
if(s==='approved'||s==='expired')location.reload()}catch{}},3000)</script></body></html>`
}
