package server

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/yeqown/go-qrcode/v2"

	"yoru/internal/firewall"
)

const (
	accessCookie  = "yoru_access"
	accessSetting = "access.token"
	lanSetting    = "access.lan"
)

func (s *Server) OnRebind(rebind func(addr string) error) { s.rebind = rebind }

func LANAddr(configured string) string {
	_, port, err := net.SplitHostPort(configured)
	if err != nil {
		return "0.0.0.0:4321"
	}
	return net.JoinHostPort("0.0.0.0", port)
}

func NewAccessToken() string {
	b := make([]byte, 24)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

var installAssets = map[string]bool{
	"/manifest.webmanifest": true, "/icon.svg": true,
	"/icon-192.png": true, "/icon-512.png": true, "/icon-maskable-512.png": true,
	"/sw.js": true, "/offline.html": true,
	"/screenshots/home.jpg": true, "/screenshots/player.jpg": true,
}

func isLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func loopbackHost(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	h = strings.ToLower(strings.Trim(h, "[]"))
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

var proxyHeaders = []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto",
	"X-Real-Ip", "Cf-Connecting-Ip", "True-Client-Ip", "Via"}

func relayed(r *http.Request) bool {
	for _, h := range proxyHeaders {
		if len(r.Header.Values(h)) > 0 {
			return true
		}
	}
	return r.ProtoMajor == 1 && r.ProtoMinor == 0 && isLoopback(r.RemoteAddr)
}

func fromHost(r *http.Request) bool {
	return isLoopback(r.RemoteAddr) && loopbackHost(r.Host) && !relayed(r)
}

func clientAddr(r *http.Request) string {
	if relayed(r) && isLoopback(r.RemoteAddr) {
		return forwardedFor(r.Header)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func forwardedFor(h http.Header) string {
	ip := func(s string) string {
		s = strings.Trim(strings.TrimSpace(s), `"`)
		if host, _, err := net.SplitHostPort(s); err == nil {
			s = host
		}
		if p := net.ParseIP(strings.Trim(s, "[]")); p != nil {
			return p.String()
		}
		return ""
	}
	last := func(values []string) string {
		if len(values) == 0 {
			return ""
		}
		parts := strings.Split(values[len(values)-1], ",")
		return parts[len(parts)-1]
	}
	if v := ip(last(h.Values("X-Forwarded-For"))); v != "" {
		return v
	}
	for pair := range strings.SplitSeq(last(h.Values("Forwarded")), ";") {
		if k, v, ok := strings.Cut(strings.TrimSpace(pair), "="); ok && strings.EqualFold(k, "for") {
			if v := ip(v); v != "" {
				return v
			}
		}
	}
	for _, name := range []string{"Cf-Connecting-Ip", "X-Real-Ip", "True-Client-Ip"} {
		if v := ip(h.Get(name)); v != "" {
			return v
		}
	}
	return ""
}

func secureRequest(r *http.Request) bool {
	return r.TLS != nil || isLoopback(r.RemoteAddr) && relayed(r) && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

type proxySeen struct {
	at           time.Time
	client, site string
}

func (s *Server) noteProxy(r *http.Request, authorized bool) {
	site := ""
	if host := cmp.Or(r.Header.Get("X-Forwarded-Host"), r.Host); authorized && !loopbackHost(host) {
		scheme := "http"
		if secureRequest(r) {
			scheme = "https"
		}
		site = scheme + "://" + strings.TrimSpace(strings.Split(host, ",")[0])
	}
	s.proxyMu.Lock()
	defer s.proxyMu.Unlock()
	if s.proxy.at.IsZero() {
		s.log.Info("a proxy on this computer passes requests on; they need the token like any other device's")
	}
	s.proxy = proxySeen{at: time.Now(), client: clientAddr(r), site: cmp.Or(site, s.proxy.site)}
}

func presented(r *http.Request, token string) bool {
	match := 0
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		match |= subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(h, "Bearer ")), []byte(token))
	}
	if t := r.URL.Query().Get("token"); t != "" {
		match |= subtle.ConstantTimeCompare([]byte(t), []byte(token))
	}
	if c, err := r.Cookie(accessCookie); err == nil {
		match |= subtle.ConstantTimeCompare([]byte(c.Value), []byte(token))
	}
	return match == 1
}

func keepCookie(w http.ResponseWriter, r *http.Request, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: "/",
		MaxAge: 365 * 24 * 3600, HttpOnly: true, Secure: secureRequest(r), SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
		if dotSegments(r.URL.Path) {
			send(w, http.StatusBadRequest, map[string]any{"error": "bad path"})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/together/") || strings.HasPrefix(r.URL.Path, "/together/") {
			next.ServeHTTP(w, r)
			return
		}
		if !safeMethod(r.Method) && !sameOrigin(r) {
			send(w, http.StatusForbidden, map[string]any{"error": "cross-site request refused"})
			return
		}
		if fromHost(r) {
			if !sameOrigin(r) && !topLevelNavigation(r) {
				send(w, http.StatusForbidden, map[string]any{"error": "cross-site request refused"})
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		proxied := isLoopback(r.RemoteAddr) && relayed(r)
		if proxied {
			s.noteProxy(r, false)
		}
		if !isLoopback(r.RemoteAddr) && s.rebind != nil && !s.bound() {
			send(w, http.StatusForbidden, map[string]any{"error": "network access is off"})
			return
		}
		if r.Method == http.MethodGet && installAssets[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		if safeMethod(r.Method) && (strings.HasPrefix(r.URL.Path, "/x/") || strings.HasPrefix(r.URL.Path, "/c/") ||
			r.URL.Path == "/api/pair/wait") {
			next.ServeHTTP(w, r)
			return
		}
		token := s.accessToken()
		if token == "" {
			send(w, http.StatusServiceUnavailable, map[string]any{
				"error": "remote access is not configured",
			})
			return
		}

		if !presented(r, token) {
			if r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html") {
				s.pairPage(w, r)
				return
			}
			w.Header().Set("WWW-Authenticate", `Bearer realm="Yoru"`)
			send(w, http.StatusUnauthorized, map[string]any{"error": "invalid or missing token"})
			return
		}

		if proxied {
			s.noteProxy(r, true)
		}
		if r.URL.Query().Get("token") != "" {
			keepCookie(w, r, accessCookie, token)
		}
		if !s.admit(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func dotSegments(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if seg == "." || seg == ".." {
			return true
		}
	}
	return false
}

func hostOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !fromHost(r) {
			send(w, http.StatusForbidden, map[string]any{"error": "change this from the host machine"})
			return
		}
		next(w, r)
	}
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

func sameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "same-origin" {
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		site := r.Header.Get("Sec-Fetch-Site")
		return site == "" || site == "none"
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	if isLoopback(r.RemoteAddr) && relayed(r) {
		forwarded, _, _ := strings.Cut(r.Header.Get("X-Forwarded-Host"), ",")
		return forwarded != "" && strings.EqualFold(u.Host, strings.TrimSpace(forwarded))
	}
	return false
}

func topLevelNavigation(r *http.Request) bool {
	dest := r.Header.Get("Sec-Fetch-Dest")
	return safeMethod(r.Method) && r.Header.Get("Sec-Fetch-Mode") == "navigate" && (dest == "document" || dest == "")
}

func lanAddrs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	var real, virtual []string
	vpn := map[string]bool{}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP.To4()
			if ip == nil || !ip.IsPrivate() {
				continue
			}
			switch {
			case vpnName.MatchString(iface.Name):
				vpn[ip.String()] = true
				virtual = append(virtual, ip.String())
			case virtualName.MatchString(iface.Name):
				virtual = append(virtual, ip.String())
			default:
				real = append(real, ip.String())
			}
		}
	}
	return orderAddrs(real, virtual, vpn, OutboundIP())
}

func orderAddrs(real, virtual []string, vpn map[string]bool, main string) []string {
	if main != "" && !vpn[main] {
		if i := slices.Index(virtual, main); i >= 0 {
			virtual = slices.Delete(slices.Clone(virtual), i, i+1)
			real = slices.Insert(slices.Clone(real), 0, main)
		} else if i := slices.Index(real, main); i > 0 {
			real = slices.Insert(slices.Delete(slices.Clone(real), i, i+1), 0, main)
		}
	}
	return append(real, virtual...)
}

var virtualName = regexp.MustCompile(`(?i)vethernet|hyper-v|wsl|virtualbox|vmware|vboxnet|docker|vmnet`)

func PairingIP() string {
	if addrs := lanAddrs(); len(addrs) > 0 {
		return addrs[0]
	}
	return OutboundIP()
}

func OutboundIP() string {
	conn, err := net.Dial("udp4", "192.0.2.1:9")
	if err != nil {
		return ""
	}
	defer conn.Close()
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return addr.IP.String()
	}
	return ""
}

func (s *Server) port() string {
	_, port, err := net.SplitHostPort(s.cfg.Addr)
	if err != nil {
		return "4321"
	}
	return port
}

func (s *Server) bound() bool {
	host, _, err := net.SplitHostPort(s.listening())
	if err != nil {
		return false
	}
	if host == "" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsUnspecified() || !ip.IsLoopback())
}

func (s *Server) accessToken() string {
	s.tokenMu.RLock()
	defer s.tokenMu.RUnlock()
	return s.token
}

func (s *Server) accessURLs() []string {
	secure, plain := []string{}, []string{}
	token := s.accessToken()
	for _, name := range s.httpsNames() {
		secure = append(secure, fmt.Sprintf("https://%s:%s/?token=%s", name, s.port(), token))
	}
	for _, host := range lanAddrs() {
		plain = append(plain, fmt.Sprintf("http://%s:%s/?token=%s", host, s.port(), token))
	}
	if s.certs != nil && s.certs.HomeBlocked() {
		return append(plain, secure...)
	}
	return append(secure, plain...)
}

func (s *Server) PairingURLs() []string {
	if !s.bound() {
		return nil
	}
	return s.accessURLs()
}

func (s *Server) access(w http.ResponseWriter, r *http.Request) {
	host := fromHost(r)
	out := map[string]any{
		"listening": s.listening(),
		"reachable": s.bound(),
		"token":     s.accessToken(),
		"urls":      s.accessURLs(),
		"canSwitch": s.rebind != nil && host,
		"host":      host,
		"away":      awayFromHome(clientAddr(r)),
	}
	s.proxyMu.Lock()
	if p := s.proxy; host && !p.at.IsZero() {
		out["proxy"] = map[string]any{"seen": p.at.Unix(), "client": p.client, "site": p.site}
	}
	s.proxyMu.Unlock()
	send(w, http.StatusOK, out)
}

func awayFromHome(addr string) bool {
	ip := net.ParseIP(addr)
	if ip == nil || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	return !onOwnNetwork(ip)
}

func onOwnNetwork(ip net.IP) bool {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && !n.IP.IsLoopback() && n.Contains(ip) {
			return true
		}
	}
	return false
}

func (s *Server) listening() string {
	if s.lanEnabled(context.Background()) {
		return LANAddr(s.cfg.Addr)
	}
	return s.cfg.Addr
}

func (s *Server) lanEnabled(ctx context.Context) bool {
	if s.store == nil {
		return false
	}
	v, _ := s.store.Setting(ctx, lanSetting)
	return v == "true"
}

func (s *Server) setAccessNetwork(w http.ResponseWriter, r *http.Request) {
	if s.rebind == nil {
		send(w, http.StatusServiceUnavailable, map[string]any{"error": "this build cannot move its listener"})
		return
	}

	var body struct {
		LAN bool `json:"lan"`
	}
	if !decode(w, r, &body) {
		return
	}

	addr := s.cfg.Addr
	if body.LAN {
		addr = LANAddr(s.cfg.Addr)
	}
	if err := s.rebind(addr); err != nil {
		s.fail(w, "move the listener", err)
		return
	}

	if err := s.store.SetSetting(r.Context(), lanSetting, strconv.FormatBool(body.LAN)); err != nil {
		s.fail(w, "save access setting", err)
		return
	}

	s.log.Info("network access", "on", body.LAN, "listening", addr, "addresses", lanAddrs())
	if body.LAN {
		go s.LogFirewall(context.WithoutCancel(r.Context()))
	}
	send(w, http.StatusOK, map[string]any{
		"listening": addr,
		"reachable": body.LAN,
		"urls":      s.accessURLs(),
	})
}

func (s *Server) firewallStatus(ctx context.Context) (firewall.Status, error) {
	port, _ := strconv.Atoi(s.port())
	exe, _ := os.Executable()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	st, err := firewall.Check(ctx, port, exe)
	return st.Keep(phoneNetwork).Lists(), err
}

func phoneNetwork(n firewall.Network) bool {
	return !vpnName.MatchString(n.Interface) && !virtualName.MatchString(n.Interface)
}

func (s *Server) LogFirewall(ctx context.Context) {
	st, err := s.firewallStatus(ctx)
	if err != nil {
		s.log.Warn("firewall check", "err", err)
		return
	}
	for _, n := range st.Networks {
		s.log.Info("network", "name", n.Name, "interface", n.Interface, "category", n.Category)
	}
	switch {
	case st.Firewall != "":
		s.log.Info("firewall", "running", st.Firewall, "hint", st.Hint, "command", st.Command)
	case !st.Supported:
	case st.Blocked():
		s.log.Warn("firewall blocks Yoru; other devices cannot connect until it is allowed in Settings › Watch on your phone")
	case !st.Reachable():
		s.log.Warn("firewall does not allow other devices on this network",
			"public", st.OnPublic(), "portRule", st.PortRule != nil)
	default:
		s.log.Info("firewall allows other devices on this network")
	}
}

func (s *Server) accessFirewall(w http.ResponseWriter, r *http.Request) {
	st, err := s.firewallStatus(r.Context())
	if err != nil {
		s.fail(w, "firewall", err)
		return
	}
	send(w, http.StatusOK, map[string]any{
		"status":    st,
		"reachable": st.Reachable(),
		"blocked":   st.Blocked(),
		"public":    st.OnPublic(),
		"addresses": lanAddrs(),
	})
}

func (s *Server) allowFirewall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Public bool `json:"public"`
	}
	if !decode(w, r, &body) {
		return
	}
	port, _ := strconv.Atoi(s.port())
	exe, _ := os.Executable()
	err := firewall.Allow(r.Context(), port, exe, body.Public)
	if errors.Is(err, firewall.ErrCancelled) {
		send(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	if err != nil {
		s.fail(w, "firewall", err)
		return
	}
	s.log.Info("firewall rule added", "rule", firewall.RuleName(port), "public", body.Public)
	go s.LogFirewall(context.WithoutCancel(r.Context()))
	s.accessFirewall(w, r)
}

func (s *Server) peersFirewall(w http.ResponseWriter, r *http.Request) {
	port := s.cfg.Torrent.PeerPort()
	exe, _ := os.Executable()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	st, err := firewall.CheckPeers(ctx, port, exe)
	if err != nil {
		s.fail(w, "firewall", err)
		return
	}
	bound := ""
	if s.engine != nil {
		bound = s.engine.Network().Adapter
	}
	st = st.Keep(func(n firewall.Network) bool {
		if virtualName.MatchString(n.Interface) {
			return false
		}
		if vpnName.MatchString(n.Interface) {
			return bound != "" && strings.EqualFold(n.Interface, bound)
		}
		return true
	})
	send(w, http.StatusOK, map[string]any{
		"status":  st.Lists(),
		"port":    port,
		"allowed": st.Reachable(),
		"blocked": st.Blocked(),
	})
}

func (s *Server) allowPeersFirewall(w http.ResponseWriter, r *http.Request) {
	port := s.cfg.Torrent.PeerPort()
	exe, _ := os.Executable()
	err := firewall.AllowPeers(r.Context(), port, exe)
	if errors.Is(err, firewall.ErrCancelled) {
		send(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	if err != nil {
		s.fail(w, "firewall", err)
		return
	}
	s.log.Info("firewall rule added", "rule", firewall.PeerRuleName(port))
	s.peersFirewall(w, r)
}

func (s *Server) openNetworkSettings(w http.ResponseWriter, r *http.Request) {
	if err := firewall.OpenNetworkSettings(); err != nil {
		s.fail(w, "open network settings", err)
		return
	}
	send(w, http.StatusOK, map[string]any{"opened": true})
}

func (s *Server) accessQR(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("url")
	if target == "" {
		urls := s.accessURLs()
		if len(urls) == 0 {
			send(w, http.StatusPreconditionFailed, map[string]any{
				"error": "no private network address found",
			})
			return
		}
		target = urls[0]
	}

	svg, err := qrSVG(target)
	if err != nil {
		s.fail(w, "qr", err)
		return
	}

	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(svg)
}

func (s *Server) rotateAccess(w http.ResponseWriter, r *http.Request) {
	token := NewAccessToken()
	if err := s.store.SetSetting(r.Context(), accessSetting, token); err != nil {
		s.fail(w, "rotate access token", err)
		return
	}
	s.tokenMu.Lock()
	s.token = token
	s.tokenMu.Unlock()
	s.pairMu.Lock()
	clear(s.pairs)
	s.pairMu.Unlock()
	if err := s.store.DeletePairedPush(r.Context()); err != nil {
		s.log.Warn("stop unpaired devices' alerts", "err", err)
	}

	send(w, http.StatusOK, map[string]any{"token": token, "urls": s.accessURLs()})
}

type bitmapWriter struct{ cells [][]bool }

func (b *bitmapWriter) Write(m qrcode.Matrix) error { b.cells = m.Bitmap(); return nil }
func (b *bitmapWriter) Close() error                { return nil }

func qrCells(content string) ([][]bool, error) {
	code, err := qrcode.New(content)
	if err != nil {
		return nil, err
	}

	var bm bitmapWriter
	if err := code.Save(&bm); err != nil {
		return nil, err
	}
	if len(bm.cells) == 0 {
		return nil, fmt.Errorf("empty qr matrix")
	}
	return bm.cells, nil
}

func qrSVG(content string) ([]byte, error) {
	cells, err := qrCells(content)
	if err != nil {
		return nil, err
	}

	const quiet = 4
	size := len(cells) + quiet*2

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges">`, size, size)
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="`, size, size)

	for y, row := range cells {
		for x, on := range row {
			if on {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x+quiet, y+quiet)
			}
		}
	}
	b.WriteString(`"/></svg>`)

	return []byte(b.String()), nil
}
