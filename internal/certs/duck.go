package certs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/caddyserver/certmagic"
	"github.com/libdns/duckdns"
	"go.uber.org/zap"

	"yoru/internal/config"
)

const duckSuffix = ".duckdns.org"

var DuckUpdateURL = "https://www.duckdns.org/update"

type duck struct {
	domain, token string
	config        *certmagic.Config
	cache         *certmagic.Cache
	ctx           context.Context
	stop          context.CancelFunc

	mu            sync.Mutex
	state         string
	problem       string
	addrProblem   string
	ip            string
	pointed       time.Time
	confirmed     time.Time
	home, homeSaw string
}

func (d *duck) ready() bool {
	for _, cert := range d.cache.AllMatchingCertificates(d.domain) {
		if cert.Leaf != nil && time.Now().Before(cert.Leaf.NotAfter) {
			return true
		}
	}
	return false
}

func (d *duck) set(state, problem string) {
	d.mu.Lock()
	d.state, d.problem = state, problem
	d.mu.Unlock()
}

const (
	StateOff     = "off"
	StateWorking = "working"
	StateReady   = "ready"
	StateError   = "error"
)

type Status struct {
	Domain     string `json:"domain"`
	State      string `json:"state"`
	Problem    string `json:"problem,omitempty"`
	Expires    int64  `json:"expires,omitempty"`
	IP         string `json:"ip,omitempty"`
	AtHome     *bool  `json:"atHome,omitempty"`
	HomeAnswer string `json:"homeAnswer,omitempty"`
}

func (s *Source) Status() Status {
	s.mu.RLock()
	d := s.duck
	s.mu.RUnlock()
	if d == nil {
		return Status{State: StateOff}
	}
	d.mu.Lock()
	out := Status{Domain: d.domain, State: d.state, Problem: d.problem, IP: d.ip, HomeAnswer: d.homeSaw}
	addr := d.addrProblem
	if d.home != "" {
		at := d.home == "yes"
		out.AtHome = &at
	}
	d.mu.Unlock()
	if out.State == StateReady && !d.ready() {
		out.State = StateWorking
		if out.Problem == "" {
			out.Problem = "the certificate has expired; a new one is being fetched"
		}
	}
	for _, cert := range d.cache.AllMatchingCertificates(d.domain) {
		if cert.Leaf != nil {
			out.Expires = cert.Leaf.NotAfter.Unix()
		}
	}
	if d.ready() {
		out.State, out.Problem = StateReady, ""
		if addr != "" {
			out.Problem = "the name's address could not be updated: " + addr
		}
	}
	return out
}

var duckName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

func DuckDomain(typed string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(typed))
	name = strings.TrimPrefix(strings.TrimPrefix(name, "https://"), "http://")
	name, _, _ = strings.Cut(name, "/")
	name, _, _ = strings.Cut(name, ":")
	name = strings.TrimSuffix(name, duckSuffix)
	if !duckName.MatchString(name) {
		return "", errors.New("the name is the part before .duckdns.org: letters, digits and dashes")
	}
	return name + duckSuffix, nil
}

func (s *Source) UseDuckDNS(typed, token string) error {
	domain, err := DuckDomain(typed)
	if err != nil {
		return err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("the token from duckdns.org is needed")
	}

	ctx, cancel := context.WithCancel(context.Background())
	d := &duck{domain: domain, token: token, ctx: ctx, stop: cancel, state: StateWorking}
	d.cache = certmagic.NewCache(certmagic.CacheOptions{
		GetConfigForCert: func(certmagic.Certificate) (*certmagic.Config, error) { return d.config, nil },
		Logger:           zap.NewNop(),
	})
	d.config = certmagic.New(d.cache, certmagic.Config{
		Storage: &certmagic.FileStorage{Path: s.dir},
		Logger:  zap.NewNop(),
		OnEvent: func(_ context.Context, event string, data map[string]any) error {
			switch event {
			case "cert_obtained":
				d.set(StateReady, "")
				s.log.Info("https certificate ready", "name", domain)
			case "cert_failed":
				err, _ := data["error"].(error)
				if err == nil {
					err = errors.New("the certificate authority refused")
				}
				if renewal, _ := data["renewal"].(bool); !renewal || !d.ready() {
					d.set(StateError, plainProblem(err, token))
				}
				s.log.Warn("https certificate", "name", domain, "err", plainProblem(err, token))
			}
			return nil
		},
	})
	ca := certmagic.LetsEncryptProductionCA
	if config.Env("ACME_STAGING") == "1" {
		ca = certmagic.LetsEncryptStagingCA
	}
	d.config.Issuers = []certmagic.Issuer{certmagic.NewACMEIssuer(d.config, certmagic.ACMEIssuer{
		CA: ca, Agreed: true, Logger: zap.NewNop(),
		DisableHTTPChallenge: true, DisableTLSALPNChallenge: true,
		DNS01Solver: &certmagic.DNS01Solver{DNSManager: certmagic.DNSManager{
			DNSProvider: &duckdns.Provider{APIToken: token},
			Logger:      zap.NewNop(),
		}},
	})}

	s.mu.Lock()
	old := s.duck
	s.duck = d
	s.mu.Unlock()
	if old != nil {
		old.stop()
		old.cache.Stop()
	}
	go s.runDuck(ctx, d)
	return nil
}

func (s *Source) StopDuckDNS() {
	s.mu.Lock()
	d := s.duck
	s.duck = nil
	s.mu.Unlock()
	if d != nil {
		d.stop()
		d.cache.Stop()
	}
}

const (
	duckRetry      = 10 * time.Minute
	duckFirstRetry = 30 * time.Second
	duckIPCheck    = 5 * time.Minute
)

var manageCert = func(ctx context.Context, cfg *certmagic.Config, domain string) error {
	return cfg.ManageAsync(ctx, []string{domain})
}

func (s *Source) runDuck(ctx context.Context, d *duck) {
	if err := manageCert(ctx, d.config, d.domain); err != nil {
		d.set(StateError, plainProblem(err, d.token))
		s.log.Warn("https certificate", "name", d.domain, "err", plainProblem(err, d.token))
	} else if d.ready() {
		d.set(StateReady, "")
		s.log.Info("https certificate ready", "name", d.domain)
	}

	wait := duckFirstRetry
	for {
		err := s.pointDuck(ctx, d)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			break
		}
		if !d.ready() {
			d.set(StateError, plainProblem(err, d.token))
		}
		s.log.Warn("duckdns address update", "name", d.domain, "err", plainProblem(err, d.token), "again_in", wait)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, duckRetry)
	}

	tick := time.NewTicker(duckIPCheck)
	defer tick.Stop()
	settled := time.After(time.Until(d.pointedAt().Add(homeSettle)))
	for {
		select {
		case <-ctx.Done():
			return
		case <-settled:
			s.checkHome(ctx, d)
		case <-tick.C:
			if err := s.pointDuck(ctx, d); err != nil {
				s.log.Warn("duckdns address update", "name", d.domain, "err", err)
			}
			if wait := time.Until(d.pointedAt().Add(homeSettle)); wait > 0 {
				settled = time.After(wait)
			} else {
				s.checkHome(ctx, d)
			}
		}
	}
}

const homeSettle = 2 * time.Minute

func (d *duck) pointedAt() time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.pointed
}

var lookupHost = net.DefaultResolver.LookupHost

func (s *Source) checkHome(ctx context.Context, d *duck) {
	d.mu.Lock()
	ip := d.ip
	d.mu.Unlock()
	if ip == "" {
		return
	}
	lookup, cancel := context.WithTimeout(ctx, 10*time.Second)
	addrs, err := lookupHost(lookup, d.domain)
	cancel()
	if ctx.Err() != nil {
		return
	}
	home := "no"
	var dnsErr *net.DNSError
	switch {
	case slices.Contains(addrs, ip):
		home = "yes"
	case err != nil && !(errors.As(err, &dnsErr) && dnsErr.IsNotFound):
		s.log.Debug("ask this network for the name", "name", d.domain, "err", err)
		return
	}
	d.mu.Lock()
	was := d.home
	d.home, d.homeSaw = home, strings.Join(addrs, ", ")
	d.mu.Unlock()
	if home == "no" && was != "no" {
		s.log.Warn("this network doesn't answer the name with this machine's address; phones here can't open it",
			"name", d.domain, "want", ip, "answered", addrs, "err", err)
	}
}

func (s *Source) CheckHome(ctx context.Context) Status {
	s.mu.RLock()
	d := s.duck
	s.mu.RUnlock()
	if d != nil {
		s.checkHome(ctx, d)
	}
	return s.Status()
}

func (s *Source) HomeBlocked() bool {
	s.mu.RLock()
	d := s.duck
	s.mu.RUnlock()
	if d == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.home == "no"
}

func (s *Source) pointDuck(ctx context.Context, d *duck) (err error) {
	defer func() {
		d.mu.Lock()
		if err != nil {
			d.addrProblem = err.Error()
		} else {
			d.addrProblem = ""
		}
		d.mu.Unlock()
	}()
	ip := s.lanIP()
	if ip == "" {
		return errors.New("this machine has no network address right now")
	}
	d.mu.Lock()
	same := d.ip == ip && time.Since(d.confirmed) < 6*time.Hour
	d.mu.Unlock()
	if same {
		return nil
	}

	q := url.Values{"domains": {strings.TrimSuffix(d.domain, duckSuffix)}, "token": {d.token}, "ip": {ip}}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, DuckUpdateURL+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach duckdns.org: %w", urlless(err))
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 64))
	if !strings.HasPrefix(string(body), "OK") {
		return errors.New("duckdns.org refused the name or token; check both on duckdns.org")
	}
	d.mu.Lock()
	if d.ip != ip {
		d.pointed = time.Now()
	}
	d.ip, d.confirmed = ip, time.Now()
	d.mu.Unlock()
	return nil
}

func urlless(err error) error {
	if ue, ok := errors.AsType[*url.Error](err); ok {
		return ue.Err
	}
	return err
}

func plainProblem(err error, token string) string {
	msg := strings.ReplaceAll(urlless(err).Error(), token, "…")
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	if len(msg) > 300 {
		msg = msg[:300] + "…"
	}
	return msg
}
