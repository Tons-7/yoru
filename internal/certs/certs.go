package certs

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

type Source struct {
	log   *slog.Logger
	dir   string
	lanIP func() string

	mu        sync.RWMutex
	files     *certFiles
	fileNames []string
	fileIPs   []net.IP
	duck      *duck
}

func New(dir string, lanIP func() string, log *slog.Logger) *Source {
	return &Source{log: log, dir: dir, lanIP: lanIP}
}

func (s *Source) TLSConfig() *tls.Config {
	return &tls.Config{GetCertificate: s.certificate, MinVersion: tls.VersionTLS12}
}

var errNoCertificate = errors.New("no certificate for this name")

func (s *Source) certificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	s.mu.RLock()
	files, d := s.files, s.duck
	s.mu.RUnlock()
	if d != nil && strings.EqualFold(hello.ServerName, d.domain) {
		cert, err := d.config.GetCertificate(hello)
		if err == nil || files == nil {
			return cert, err
		}
	}
	if files != nil {
		return files.get()
	}
	return nil, errNoCertificate
}

func (s *Source) Names() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []string
	if s.duck != nil && s.duck.ready() {
		out = append(out, s.duck.domain)
	}
	out = append(out, s.fileNames...)
	for _, ip := range s.fileIPs {
		if hasAddr(ip) {
			out = append(out, ip.String())
		}
	}
	return out
}

func (s *Source) UseFiles(cert, key string) error {
	files := &certFiles{cert: cert, key: key}
	pair, err := files.get()
	if err != nil {
		return err
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return err
	}
	var names []string
	for _, n := range leaf.DNSNames {
		lower := strings.ToLower(n)
		if strings.Contains(n, "*") || lower == "localhost" || strings.HasSuffix(lower, ".localhost") || net.ParseIP(n) != nil {
			continue
		}
		names = append(names, n)
	}
	var ips []net.IP
	for _, ip := range leaf.IPAddresses {
		if ip.To4() != nil && ip.IsPrivate() {
			ips = append(ips, ip)
		}
	}
	s.mu.Lock()
	s.files, s.fileNames, s.fileIPs = files, names, ips
	s.mu.Unlock()
	return nil
}

func hasAddr(ip net.IP) bool {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.Equal(ip) {
			return true
		}
	}
	return false
}

type certFiles struct {
	cert, key string

	mu     sync.Mutex
	loaded *tls.Certificate
	stamp  time.Time
	badAt  time.Time
	badErr error
}

func (c *certFiles) get() (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	info, err := os.Stat(c.cert)
	if err == nil && c.loaded != nil && info.ModTime().Equal(c.stamp) {
		return c.loaded, nil
	}
	if err == nil && c.badErr != nil && info.ModTime().Equal(c.badAt) {
		if c.loaded != nil {
			return c.loaded, nil
		}
		return nil, c.badErr
	}
	pair, err := tls.LoadX509KeyPair(c.cert, c.key)
	if err != nil {
		if info != nil {
			c.badAt, c.badErr = info.ModTime(), err
		}
		if c.loaded != nil {
			return c.loaded, nil
		}
		return nil, err
	}
	c.badErr = nil
	c.loaded = &pair
	if info != nil {
		c.stamp = info.ModTime()
	}
	return c.loaded, nil
}
