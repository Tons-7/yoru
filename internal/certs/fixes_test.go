package certs

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/caddyserver/certmagic"
)

// A second save replaces the running DuckDNS manager and stops it; none is left pushing the address for good.
func TestSecondDuckDNSSetupStopsTheFirst(t *testing.T) {
	var calls []url.Values
	duckServer(t, "OK", &calls)
	// No certificate: a real order went to Let's Encrypt, and its lock file outlived the test's folder.
	was := manageCert
	manageCert = func(context.Context, *certmagic.Config, string) error { return nil }
	t.Cleanup(func() { manageCert = was })
	s := New(t.TempDir(), func() string { return "" }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := s.UseDuckDNS("myyoru", "token-1"); err != nil {
		t.Fatal(err)
	}
	s.mu.RLock()
	first := s.duck
	s.mu.RUnlock()
	if err := s.UseDuckDNS("myyoru", "token-2"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.StopDuckDNS)
	if first.ctx.Err() == nil {
		t.Fatal("the replaced manager is still running")
	}
	s.mu.RLock()
	current := s.duck
	s.mu.RUnlock()
	if current == first || current.token != "token-2" {
		t.Fatal("the new manager did not take over")
	}
}

// A router with DNS rebind protection drops the name's home address: said so, and cleared when it answers.
func TestARouterThatWontAnswerTheNameIsNoticed(t *testing.T) {
	var calls []url.Values
	duckServer(t, "OK", &calls)
	was := manageCert
	manageCert = func(context.Context, *certmagic.Config, string) error { return nil }
	t.Cleanup(func() { manageCert = was })
	answer := []string{}
	var failure error
	lookup := lookupHost
	lookupHost = func(context.Context, string) ([]string, error) { return answer, failure }
	t.Cleanup(func() { lookupHost = lookup })

	s := New(t.TempDir(), func() string { return "" }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := s.UseDuckDNS("myyoru", "token"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.StopDuckDNS)
	if st := s.Status(); st.AtHome != nil {
		t.Fatalf("asked before there was an address: %+v", st)
	}
	s.mu.RLock()
	d := s.duck
	s.mu.RUnlock()
	d.mu.Lock()
	d.ip = "192.168.1.10"
	d.mu.Unlock()

	if st := s.CheckHome(context.Background()); st.AtHome == nil || *st.AtHome || !s.HomeBlocked() {
		t.Errorf("no answer at home, yet %+v", st)
	}
	answer = []string{"192.168.1.10"}
	if st := s.CheckHome(context.Background()); st.AtHome == nil || !*st.AtHome || s.HomeBlocked() {
		t.Errorf("the router answers now, yet %+v", st)
	}

	// A slow resolver is no verdict; a router dropping the answer reads as the name not existing.
	answer, failure = nil, &net.DNSError{Err: "i/o timeout", Name: "myyoru.duckdns.org", IsTimeout: true}
	if st := s.CheckHome(context.Background()); st.AtHome == nil || !*st.AtHome || s.HomeBlocked() {
		t.Errorf("a timeout counted as the router blocking the name: %+v", st)
	}
	failure = &net.DNSError{Err: "no such host", Name: "myyoru.duckdns.org", IsNotFound: true}
	if st := s.CheckHome(context.Background()); st.AtHome == nil || *st.AtHome || !s.HomeBlocked() {
		t.Errorf("a dropped answer wasn't noticed: %+v", st)
	}
}

// An owner certificate for this machine's own address pairs over https too.
func TestOwnCertificateForTheLANAddressNamesIt(t *testing.T) {
	ip := ownPrivateIP()
	if ip == nil {
		t.Skip("no private address on this machine")
	}
	cert, key := selfSignedFor(t, []string{"pc.local"}, []net.IP{ip, net.ParseIP("10.99.99.99")})
	s := New(t.TempDir(), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := s.UseFiles(cert, key); err != nil {
		t.Fatal(err)
	}
	names := s.Names()
	if !slices.Contains(names, "pc.local") || !slices.Contains(names, ip.String()) {
		t.Fatalf("names = %v, want the DNS name and this machine's address", names)
	}
	if slices.Contains(names, "10.99.99.99") {
		t.Fatal("an address that is not this machine's was offered")
	}
}

func ownPrivateIP() net.IP {
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && n.IP.IsPrivate() {
			return n.IP.To4()
		}
	}
	return nil
}

func selfSignedFor(t *testing.T, dns []string, ips []net.IP) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: dns[0]},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		DNSNames: dns, IPAddresses: ips, KeyUsage: x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalECPrivateKey(key)
	dir := t.TempDir()
	certPath, keyPath = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600)
	return certPath, keyPath
}
