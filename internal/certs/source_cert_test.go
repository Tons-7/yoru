package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func ownerCert(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "yoru.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{"yoru.test"},
		IPAddresses:  []net.IP{net.ParseIP("10.254.253.252")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600)
	return certPath, keyPath
}

func TestCertificateComesFromTheOwnerFiles(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, func() string { return "" }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	hello := &tls.ClientHelloInfo{ServerName: "yoru.test"}
	if _, err := s.certificate(hello); !errors.Is(err, errNoCertificate) {
		t.Fatalf("with nothing set up: %v", err)
	}
	cert, key := ownerCert(t, dir)
	if err := s.UseFiles(cert, key); err != nil {
		t.Fatal(err)
	}
	if got, err := s.certificate(hello); err != nil || got == nil {
		t.Fatalf("owner certificate: %v %v", got, err)
	}
	names := s.Names()
	if !slices.Contains(names, "yoru.test") {
		t.Errorf("names = %v, missing the certificate's name", names)
	}
	if slices.Contains(names, "10.254.253.252") {
		t.Errorf("names = %v, offers an address this computer doesn't have", names)
	}
}
