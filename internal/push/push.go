// Package push sends Web Push alerts, so a new episode reaches a phone with Yoru closed. Encrypted to each
// device (RFC 8291) and signed as Yoru's (VAPID, RFC 8292), they pass the push services unread.
package push

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"yoru/internal/store"
)

var b64 = base64.RawURLEncoding

// Keys is Yoru's VAPID key pair: a subscription takes messages only from the key it was made with.
type Keys struct {
	priv *ecdsa.PrivateKey
}

// NewKeys makes a fresh pair; Encode is what gets stored.
func NewKeys() (Keys, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	return Keys{priv}, err
}

// Encode is the private key as stored, base64url of its 32-byte scalar.
func (k Keys) Encode() string {
	raw, _ := k.priv.Bytes()
	return b64.EncodeToString(raw)
}

// DecodeKeys reads a stored pair back.
func DecodeKeys(s string) (Keys, error) {
	raw, err := b64.DecodeString(s)
	if err != nil {
		return Keys{}, errors.New("push: stored key is damaged")
	}
	priv, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), raw)
	if err != nil {
		return Keys{}, fmt.Errorf("push: stored key is damaged: %w", err)
	}
	return Keys{priv}, nil
}

// Public is the applicationServerKey a page subscribes with: the uncompressed point, base64url.
func (k Keys) Public() string {
	pub, _ := k.priv.PublicKey.ECDH()
	return b64.EncodeToString(pub.Bytes())
}

// Alert is what a device shows; Path is the page a tap opens, under the address the device subscribed from.
type Alert struct {
	Title, Body, Tag, Path string
}

// Sender sends alerts to every stored subscription and forgets the ones the push service says are gone.
type Sender struct {
	keys  Keys
	store *store.Store
	http  *http.Client
	log   *slog.Logger
	// Contact named in the VAPID claim; push services may write to it about a misbehaving sender.
	subject string
}

func NewSender(keys Keys, st *store.Store, log *slog.Logger) *Sender {
	return &Sender{keys: keys, store: st, http: &http.Client{Timeout: 30 * time.Second}, log: log,
		subject: "https://github.com/Tons-7/yoru"}
}

// PublicKey is the applicationServerKey pages subscribe with.
func (s *Sender) PublicKey() string { return s.keys.Public() }

// SendTo delivers the alert to one device, as a test from Settings does.
func (s *Sender) SendTo(ctx context.Context, sub store.PushSubscription, a Alert) error {
	err := s.sendOne(ctx, sub, a)
	if errors.Is(err, errGone) {
		s.store.DeletePushSubscription(ctx, sub.Endpoint)
	}
	return err
}

// Send delivers the alert to every subscribed device; one failing doesn't stop the others.
func (s *Sender) Send(ctx context.Context, a Alert) {
	subs, err := s.store.PushSubscriptions(ctx)
	if err != nil {
		s.log.Warn("push: read subscriptions", "err", err)
		return
	}
	for _, sub := range subs {
		err := s.sendOne(ctx, sub, a)
		switch {
		case errors.Is(err, errGone):
			// Unsubscribed, expired, or the app removed: a dead endpoint stays dead.
			s.store.DeletePushSubscription(ctx, sub.Endpoint)
		case err != nil:
			s.log.Warn("push: send", "endpoint", hostOf(sub.Endpoint), "err", err)
		}
	}
}

var errGone = errors.New("push: subscription gone")

func (s *Sender) sendOne(ctx context.Context, sub store.PushSubscription, a Alert) error {
	// The declarative form (RFC 8030's "web_push": 8030): Safari shows it without running any script, and
	// browsers without that hand the same JSON to the service worker.
	payload, err := json.Marshal(map[string]any{
		"web_push": 8030,
		"notification": map[string]any{
			"title": a.Title, "body": a.Body, "tag": a.Tag, "lang": "en",
			"navigate": strings.TrimRight(sub.Origin, "/") + a.Path,
		},
	})
	if err != nil {
		return err
	}
	uaPublic, err := b64.DecodeString(sub.P256dh)
	if err != nil {
		return errGone
	}
	auth, err := b64.DecodeString(sub.Auth)
	if err != nil {
		return errGone
	}
	ephemeral, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	salt := make([]byte, 16)
	rand.Read(salt)
	body, err := encrypt(payload, uaPublic, auth, ephemeral, salt)
	if err != nil {
		return errGone
	}
	token, err := s.vapid(sub.Endpoint)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return errGone
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Encoding", "aes128gcm")
	// A day: an episode alert is still worth showing to a phone that was off overnight.
	req.Header.Set("TTL", "86400")
	req.Header.Set("Urgency", "normal")
	req.Header.Set("Authorization", fmt.Sprintf("vapid t=%s, k=%s", token, s.keys.Public()))
	res, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusGone:
		return errGone
	case res.StatusCode >= 300:
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

// vapid is the signed claim (RFC 8292) that the message comes from the key the subscription was made for.
func (s *Sender) vapid(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	header := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, _ := json.Marshal(map[string]any{
		"aud": u.Scheme + "://" + u.Host,
		"exp": time.Now().Add(12 * time.Hour).Unix(),
		"sub": s.subject,
	})
	signing := header + "." + b64.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signing))
	r, sig, err := ecdsa.Sign(rand.Reader, s.keys.priv, digest[:])
	if err != nil {
		return "", err
	}
	// ES256 is r and s side by side, 32 bytes each, not DER.
	raw := append(r.FillBytes(make([]byte, 32)), sig.FillBytes(make([]byte, 32))...)
	return signing + "." + b64.EncodeToString(raw), nil
}

// recordSize is the one record a short alert fits in.
const recordSize = 4096

// encrypt is RFC 8291's aes128gcm message for one device: its public key and auth secret, a fresh key pair of
// ours whose public half goes in the header, and a random salt.
func encrypt(payload, uaPublic, authSecret []byte, ours *ecdh.PrivateKey, salt []byte) ([]byte, error) {
	ua, err := ecdh.P256().NewPublicKey(uaPublic)
	if err != nil {
		return nil, err
	}
	shared, err := ours.ECDH(ua)
	if err != nil {
		return nil, err
	}
	asPublic := ours.PublicKey().Bytes()
	keyInfo := "WebPush: info\x00" + string(uaPublic) + string(asPublic)
	ikm, err := hkdf.Key(sha256.New, shared, authSecret, keyInfo, 32)
	if err != nil {
		return nil, err
	}
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// 0x02 marks the last (and only) record, with no padding after it.
	sealed := gcm.Seal(nil, nonce, append(append([]byte{}, payload...), 0x02), nil)
	if len(sealed)+86 > recordSize {
		return nil, errors.New("push: alert too long for one record")
	}

	var out bytes.Buffer
	out.Write(salt)
	binary.Write(&out, binary.BigEndian, uint32(recordSize))
	out.WriteByte(byte(len(asPublic)))
	out.Write(asPublic)
	out.Write(sealed)
	return out.Bytes(), nil
}

func hostOf(endpoint string) string {
	if u, err := url.Parse(endpoint); err == nil {
		return u.Host
	}
	return ""
}
