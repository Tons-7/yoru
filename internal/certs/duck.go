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

// DuckUpdateURL is DuckDNS's one endpoint; a variable so tests can stand in for it.
var DuckUpdateURL = "https://www.duckdns.org/update"

// duck keeps one DuckDNS name pointed at this machine and certified.
type duck struct {
	domain, token string
	config        *certmagic.Config
	cache         *certmagic.Cache
	ctx           context.Context
	stop          context.CancelFunc

	mu      sync.Mutex
	state   string // working | ready | error
	problem string
	ip      string
	// When ip was last set, as a resolver may hold the old answer a while.
	pointed time.Time
	// Whether this network's resolver answers the name with ip: "" unasked, "yes" or "no", and what it said.
	home, homeSaw string
}

// ready is whether a certificate is loaded, however it got there: fetched just now or read from disk.
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

// Status is what Settings shows about the automatic certificate.
type Status struct {
	Domain  string `json:"domain"`
	State   string `json:"state"`
	Problem string `json:"problem,omitempty"`
	// Expires is when the certificate runs out, unix seconds; renewed well before.
	Expires int64  `json:"expires,omitempty"`
	IP      string `json:"ip,omitempty"`
	// AtHome: this network's resolver answers the name with IP; nil until asked. HomeAnswer is what it said.
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
	if d.home != "" {
		at := d.home == "yes"
		out.AtHome = &at
	}
	d.mu.Unlock()
	// Ready is a certificate that still holds; one that ran out is being fetched again, or failed to be.
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
	// A loaded certificate is ready, whatever the last attempt said.
	if d.ready() {
		out.State, out.Problem = StateReady, ""
	}
	return out
}

var duckName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// DuckDomain turns what a person types ("myyoru", "MyYoru.duckdns.org", a pasted URL) into the full name.
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

// UseDuckDNS points the name here and keeps a Let's Encrypt certificate for it; Status reports progress.
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
		// Issuance runs in the library's background mode, which reports only through events.
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
				// A failed renewal leaves the current certificate serving, unless it has already run out.
				if renewal, _ := data["renewal"].(bool); !renewal || !d.ready() {
					d.set(StateError, plainProblem(err, token))
				}
				s.log.Warn("https certificate", "name", domain, "err", plainProblem(err, token))
			}
			return nil
		},
	})
	ca := certmagic.LetsEncryptProductionCA
	// Let's Encrypt's staging CA, for trying the flow without spending real issuance limits.
	if config.Env("ACME_STAGING") == "1" {
		ca = certmagic.LetsEncryptStagingCA
	}
	d.config.Issuers = []certmagic.Issuer{certmagic.NewACMEIssuer(d.config, certmagic.ACMEIssuer{
		CA: ca, Agreed: true, Logger: zap.NewNop(),
		// The name points at a private address, so only the DNS challenge can prove it.
		DisableHTTPChallenge: true, DisableTLSALPNChallenge: true,
		DNS01Solver: &certmagic.DNS01Solver{DNSManager: certmagic.DNSManager{
			DNSProvider: &duckdns.Provider{APIToken: token},
			Logger:      zap.NewNop(),
		}},
	})}

	// Swapped under the lock and the one replaced stopped: two saves at once left one running for good.
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

// StopDuckDNS turns the automatic certificate off; the files on disk stay for next time.
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

// manageCert starts the certificate's upkeep; a var so tests never ask Let's Encrypt for one.
var manageCert = func(ctx context.Context, cfg *certmagic.Config, domain string) error {
	return cfg.ManageAsync(ctx, []string{domain})
}

func (s *Source) runDuck(ctx context.Context, d *duck) {
	// The certificate on disk serves at once; async, so it never prompts, and a new one comes via OnEvent.
	if err := manageCert(ctx, d.config, d.domain); err != nil {
		d.set(StateError, plainProblem(err, d.token))
		s.log.Warn("https certificate", "name", d.domain, "err", plainProblem(err, d.token))
	} else if d.ready() {
		d.set(StateReady, "")
		s.log.Info("https certificate ready", "name", d.domain)
	}

	// Until it succeeds, soon at first: Wi-Fi comes up a moment after Yoru does.
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

// How long after setting the address a resolver's answer is trusted: DuckDNS answers live a minute.
const homeSettle = 2 * time.Minute

func (d *duck) pointedAt() time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.pointed
}

// lookupHost asks the system's resolver, at home the router phones ask too; a var so tests can answer.
var lookupHost = net.DefaultResolver.LookupHost

// checkHome asks whether this network answers the name with this machine's address. Routers with DNS rebind
// protection drop answers that point at a home address, and then no phone here can open the name.
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
		// A timeout says nothing of the router (one dropping answers says "not found"): the last answer stands.
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

// CheckHome asks the network again, for Settings' Check again.
func (s *Source) CheckHome(ctx context.Context) Status {
	s.mu.RLock()
	d := s.duck
	s.mu.RUnlock()
	if d != nil {
		s.checkHome(ctx, d)
	}
	return s.Status()
}

// HomeBlocked: this network won't answer the DuckDNS name with this machine's address.
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

// pointDuck sets the name's address to this machine's, when it is not already.
func (s *Source) pointDuck(ctx context.Context, d *duck) error {
	ip := s.lanIP()
	if ip == "" {
		return errors.New("this machine has no network address right now")
	}
	d.mu.Lock()
	same := d.ip == ip
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
	d.ip, d.pointed = ip, time.Now()
	d.mu.Unlock()
	return nil
}

// urlless drops the request URL from an error: it carries the token.
func urlless(err error) error {
	if ue, ok := errors.AsType[*url.Error](err); ok {
		return ue.Err
	}
	return err
}

// plainProblem is the error as Settings shows it: one line, no token.
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
