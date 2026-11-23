package server

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"yoru/internal/certs"
)

func (s *Server) SetCerts(src *certs.Source) { s.certs = src }

func (s *Server) httpsNames() []string {
	if s.certs == nil {
		return nil
	}
	return s.certs.Names()
}

type dualListener struct {
	net.Listener
	cfg   *tls.Config
	conns chan net.Conn
	errs  chan error
	done  chan struct{}
	once  sync.Once
}

func DualListener(ln net.Listener, cfg *tls.Config) net.Listener {
	d := &dualListener{
		Listener: ln, cfg: cfg,
		conns: make(chan net.Conn), errs: make(chan error, 1), done: make(chan struct{}),
	}
	go d.accept()
	return d
}

func (d *dualListener) accept() {
	for {
		conn, err := d.Listener.Accept()
		if err != nil {
			select {
			case d.errs <- err:
			case <-d.done:
				return
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		go d.sniff(conn)
	}
}

const tlsHandshake = 0x16

func (d *dualListener) sniff(conn net.Conn) {
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(conn)
	first, err := r.Peek(1)
	if err != nil {
		conn.Close()
		return
	}
	conn.SetReadDeadline(time.Time{})

	var out net.Conn = &peeked{Conn: conn, r: r}
	if first[0] == tlsHandshake {
		out = tls.Server(out, d.cfg)
	}
	select {
	case d.conns <- out:
	case <-d.done:
		conn.Close()
	}
}

func (d *dualListener) Accept() (net.Conn, error) {
	select {
	case conn := <-d.conns:
		return conn, nil
	case err := <-d.errs:
		return nil, err
	case <-d.done:
		return nil, net.ErrClosed
	}
}

func (d *dualListener) Close() error {
	d.once.Do(func() { close(d.done) })
	return d.Listener.Close()
}

type peeked struct {
	net.Conn
	r *bufio.Reader
}

func (p *peeked) Read(b []byte) (int, error) { return p.r.Read(b) }

func (p *peeked) CloseWrite() error {
	if cw, ok := p.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}

func (p *peeked) ReadFrom(r io.Reader) (int64, error) {
	if rf, ok := p.Conn.(io.ReaderFrom); ok {
		return rf.ReadFrom(r)
	}
	return io.Copy(struct{ io.Writer }{p.Conn}, r)
}

const (
	duckDomainSetting = "https.duckdns.domain"
	duckTokenSetting  = "https.duckdns.token"
)

func (s *Server) StartDuckDNS(ctx context.Context) {
	if s.certs == nil {
		return
	}
	domain, _ := s.store.Setting(ctx, duckDomainSetting)
	token, _ := s.store.Setting(ctx, duckTokenSetting)
	if domain == "" || token == "" {
		return
	}
	if err := s.certs.UseDuckDNS(domain, token); err != nil {
		s.log.Warn("duckdns", "err", err)
	}
}

func (s *Server) accessHTTPS(w http.ResponseWriter, r *http.Request) {
	if s.certs == nil {
		send(w, http.StatusOK, certs.Status{State: certs.StateOff})
		return
	}
	send(w, http.StatusOK, s.certs.Status())
}

func (s *Server) checkAccessHTTPS(w http.ResponseWriter, r *http.Request) {
	if s.certs == nil {
		send(w, http.StatusOK, certs.Status{State: certs.StateOff})
		return
	}
	send(w, http.StatusOK, s.certs.CheckHome(r.Context()))
}

func (s *Server) setAccessHTTPS(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Domain string `json:"domain"`
		Token  string `json:"token"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || s.certs == nil {
		send(w, http.StatusBadRequest, map[string]any{"error": "domain and token are needed"})
		return
	}
	if strings.TrimSpace(body.Domain) == "" {
		s.certs.StopDuckDNS()
		if err := s.store.SetSettings(r.Context(), map[string]string{duckDomainSetting: "", duckTokenSetting: ""}); err != nil {
			s.fail(w, "https off", err)
			return
		}
		send(w, http.StatusOK, s.certs.Status())
		return
	}
	token := strings.TrimSpace(body.Token)
	if token == "" {
		token, _ = s.store.Setting(r.Context(), duckTokenSetting)
	}
	oldDomain, _ := s.store.Setting(r.Context(), duckDomainSetting)
	oldToken, _ := s.store.Setting(r.Context(), duckTokenSetting)
	if err := s.certs.UseDuckDNS(body.Domain, token); err != nil {
		send(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	domain, _ := certs.DuckDomain(body.Domain)
	if err := s.store.SetSettings(r.Context(), map[string]string{duckDomainSetting: domain, duckTokenSetting: token}); err != nil {
		if oldDomain == "" || s.certs.UseDuckDNS(oldDomain, oldToken) != nil {
			s.certs.StopDuckDNS()
		}
		s.fail(w, "save https", err)
		return
	}
	send(w, http.StatusOK, s.certs.Status())
}
