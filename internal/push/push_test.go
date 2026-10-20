package push

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/json"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"yoru/internal/db"
	"yoru/internal/store"
)

func dec(t *testing.T, s string) []byte {
	t.Helper()
	b, err := b64.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// RFC 8291's own worked example (Appendix A), byte for byte.
func TestEncryptMatchesTheRFCExample(t *testing.T) {
	asPriv, err := ecdh.P256().NewPrivateKey(dec(t, "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := encrypt(
		dec(t, "V2hlbiBJIGdyb3cgdXAsIEkgd2FudCB0byBiZSBhIHdhdGVybWVsb24"),
		dec(t, "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"),
		dec(t, "BTBZMqHH6r4Tts7J_aSIgg"), asPriv, dec(t, "DGv6ra1nlYgDCS1FRnbzlw"))
	if err != nil {
		t.Fatal(err)
	}
	// Header and ciphertext as the RFC lists them, joined as bytes (86 isn't a multiple of 3, so not as text).
	want := append(dec(t, "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8"),
		dec(t, "8pfeW0KbunFT06SuDKoJH9Ql87S1QUrdirN6GcG7sFz1y1sqLgVi1VhjVkHsUoEsbI_0LpXMuGvnzQ")...)
	if string(got) != string(want) {
		t.Errorf("body\n got %s\nwant %s", b64.EncodeToString(got), b64.EncodeToString(want))
	}
}

// Stored keys come back as the same pair, and the VAPID claim verifies with the public half a page gets.
func TestKeysAndVAPID(t *testing.T) {
	keys, err := NewKeys()
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodeKeys(keys.Encode())
	if err != nil || back.Public() != keys.Public() {
		t.Fatalf("decoded pair differs: %v", err)
	}
	token, err := (&Sender{keys: back, subject: "https://example.com"}).vapid("https://push.example.com/send/abc")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	var claims map[string]any
	json.Unmarshal(dec(t, parts[1]), &claims)
	if len(parts) != 3 || claims["aud"] != "https://push.example.com" || claims["sub"] != "https://example.com" {
		t.Fatalf("token %q, claims %v", token, claims)
	}
	sig := dec(t, parts[2])
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	// Verified with the public key exactly as a page receives it.
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), dec(t, keys.Public()))
	if err != nil {
		t.Fatal(err)
	}
	if !ecdsa.Verify(pub, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Error("the VAPID signature does not verify")
	}
}

// Send reaches every device, encrypted with the declarative payload, and forgets an endpoint that is gone.
func TestSendDeliversAndForgetsGoneDevices(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := conn.Migrate(); err != nil {
		t.Fatal(err)
	}
	st := store.New(conn)

	device, _ := ecdh.P256().GenerateKey(nil)
	var got []*http.Request
	push := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r)
		io.ReadAll(r.Body)
		if strings.HasSuffix(r.URL.Path, "/gone") {
			w.WriteHeader(http.StatusGone)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(push.Close)
	for _, path := range []string{"/live", "/gone"} {
		st.SavePushSubscription(context.Background(), store.PushSubscription{
			Endpoint: push.URL + path, P256dh: b64.EncodeToString(device.PublicKey().Bytes()),
			Auth: b64.EncodeToString(make([]byte, 16)), Origin: "https://yoru.example",
		})
	}
	keys, _ := NewKeys()
	NewSender(keys, st, slog.New(slog.DiscardHandler)).Send(context.Background(), Alert{Title: "Episode 7 is available", Path: "/anime/1"})

	if len(got) != 2 || got[0].Header.Get("Content-Encoding") != "aes128gcm" || !strings.HasPrefix(got[0].Header.Get("Authorization"), "vapid t=") {
		t.Fatalf("requests: %d, headers %v", len(got), got[0].Header)
	}
	if subs, _ := st.PushSubscriptions(context.Background()); len(subs) != 1 || !strings.HasSuffix(subs[0].Endpoint, "/live") {
		t.Errorf("subscriptions left: %+v; want the gone one forgotten", subs)
	}
}
