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
	"strconv"
	"strings"
	"sync"
	"time"

	"yoru/internal/store"
)

var b64 = base64.RawURLEncoding

type Keys struct {
	priv *ecdsa.PrivateKey
}

func NewKeys() (Keys, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	return Keys{priv}, err
}

func (k Keys) Encode() string {
	raw, _ := k.priv.Bytes()
	return b64.EncodeToString(raw)
}

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

func (k Keys) Public() string {
	pub, _ := k.priv.PublicKey.ECDH()
	return b64.EncodeToString(pub.Bytes())
}

type Alert struct {
	Title, Body, Tag, Path string
	Image                  string
	Badge                  int
	Actions                []string
	AnimeID, Episode       int
}

type Sender struct {
	keys    Keys
	store   *store.Store
	http    *http.Client
	log     *slog.Logger
	subject string
}

func NewSender(keys Keys, st *store.Store, log *slog.Logger) *Sender {
	return &Sender{keys: keys, store: st, http: &http.Client{Timeout: 30 * time.Second}, log: log,
		subject: "https://github.com/Tons-7/yoru"}
}

func (s *Sender) PublicKey() string { return s.keys.Public() }

func (s *Sender) SendTo(ctx context.Context, sub store.PushSubscription, a Alert) error {
	err := s.sendOne(ctx, sub, a)
	if errors.Is(err, errGone) {
		s.forget(ctx, sub.Endpoint)
	}
	return err
}

func (s *Sender) forget(ctx context.Context, endpoint string) {
	if err := s.store.DeletePushSubscription(ctx, endpoint); err != nil {
		s.log.Warn("push: forget a dead subscription", "endpoint", hostOf(endpoint), "err", err)
	}
}

func (s *Sender) Send(ctx context.Context, a Alert) {
	subs, err := s.store.PushSubscriptions(ctx)
	if err != nil {
		s.log.Warn("push: read subscriptions", "err", err)
		return
	}
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	for _, sub := range subs {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			err := s.sendOne(ctx, sub, a)
			switch {
			case errors.Is(err, errGone):
				s.forget(ctx, sub.Endpoint)
			case err != nil:
				s.log.Warn("push: send", "endpoint", hostOf(sub.Endpoint), "err", err)
			}
		})
	}
	wg.Wait()
}

var (
	errGone    = errors.New("push: subscription gone")
	errTooLong = errors.New("push: alert too long for one record")
)

func message(origin string, a Alert) map[string]any {
	note := map[string]any{
		"title": a.Title, "body": a.Body, "tag": a.Tag, "lang": "en",
		"navigate": strings.TrimRight(origin, "/") + a.Path,
	}
	if a.Badge > 0 {
		note["app_badge"] = strconv.Itoa(a.Badge)
	}
	msg := map[string]any{"web_push": 8030, "notification": note}
	if a.Image != "" || len(a.Actions) > 0 {
		msg["yoru"] = map[string]any{"image": a.Image, "actions": a.Actions, "animeId": a.AnimeID, "episode": a.Episode}
	}
	return msg
}

func (s *Sender) sendOne(ctx context.Context, sub store.PushSubscription, a Alert) error {
	payload, err := json.Marshal(message(sub.Origin, a))
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
	for errors.Is(err, errTooLong) && a.Body != "" {
		runes := []rune(a.Body)
		a.Body = string(runes[:len(runes)*3/4]) + "…"
		if len(runes) < 8 {
			a.Body = ""
		}
		if payload, err = json.Marshal(message(sub.Origin, a)); err != nil {
			return err
		}
		body, err = encrypt(payload, uaPublic, auth, ephemeral, salt)
	}
	if err != nil {
		return err
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
	raw := append(r.FillBytes(make([]byte, 32)), sig.FillBytes(make([]byte, 32))...)
	return signing + "." + b64.EncodeToString(raw), nil
}

const recordSize = 4096

func encrypt(payload, uaPublic, authSecret []byte, ours *ecdh.PrivateKey, salt []byte) ([]byte, error) {
	ua, err := ecdh.P256().NewPublicKey(uaPublic)
	if err != nil {
		return nil, errGone
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
	sealed := gcm.Seal(nil, nonce, append(append([]byte{}, payload...), 0x02), nil)
	if len(sealed)+86 > recordSize {
		return nil, errTooLong
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
