package anilist

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

type Archive interface {
	LoadAnswer(ctx context.Context, key string) (body []byte, at time.Time, ok bool)
	SaveAnswer(ctx context.Context, key string, body []byte) error
}

func (c *Client) SetArchive(a Archive) {
	c.mu.Lock()
	c.archive = a
	c.mu.Unlock()
}

func (c *Client) archiver() Archive {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.archive
}

const downFor = 30 * time.Second

func (c *Client) markDown() {
	c.mu.Lock()
	c.downUntil = time.Now().Add(downFor)
	c.mu.Unlock()
}

func (c *Client) Down() bool { return c.isDown() }

func (c *Client) isDown() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().Before(c.downUntil)
}

func archiveKey(body []byte, authed bool) string {
	sum := sha256.New()
	sum.Write(body)
	if authed {
		sum.Write([]byte{1})
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func Unreachable(ctx context.Context, err error) bool { return unreachable(ctx, err) }

func unreachable(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	if gql, ok := errors.AsType[Errors](err); ok {
		for _, e := range gql {
			if e.Status >= 500 || e.Status == 403 || e.Status == 429 {
				return true
			}
		}
		return false
	}
	return true
}

type servedKey struct{}

type Served struct {
	mu         sync.Mutex
	live       bool
	saved      time.Time
	refreshing bool
}

func WithServed(ctx context.Context) (context.Context, *Served) {
	s := &Served{}
	return context.WithValue(ctx, servedKey{}, s), s
}

func servedFrom(ctx context.Context) *Served {
	s, _ := ctx.Value(servedKey{}).(*Served)
	return s
}

func UsedSaved(ctx context.Context) bool {
	s := servedFrom(ctx)
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.saved.IsZero() || s.refreshing
}

func NoteRefreshing(ctx context.Context) {
	servedFrom(ctx).noteRefreshing()
}

func (s *Served) Refreshing() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refreshing
}

func (s *Served) noteRefreshing() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.refreshing = true
	s.mu.Unlock()
}

func NoteSaved(ctx context.Context, at time.Time) {
	servedFrom(ctx).noteSaved(at)
}

func (s *Served) Result() (live bool, saved time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.live, s.saved
}

func (s *Served) noteLive() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.live = true
	s.mu.Unlock()
}

func (s *Served) noteSaved(at time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.saved.IsZero() || at.Before(s.saved) {
		s.saved = at
	}
	s.mu.Unlock()
}

type ownKey struct{}

func ownAnswer(ctx context.Context) context.Context { return context.WithValue(ctx, ownKey{}, true) }

func keepsAnswers(ctx context.Context) bool { return ctx.Value(ownKey{}) == nil }

func (c *Client) fromArchive(ctx context.Context, key string, out any) bool {
	served, archive := servedFrom(ctx), c.archiver()
	if served == nil || archive == nil || !keepsAnswers(ctx) {
		return false
	}
	body, at, ok := archive.LoadAnswer(ctx, key)
	if !ok || decodeInto(body, out) != nil {
		return false
	}
	served.noteSaved(at)
	return true
}

const recheckQuery = `query Recheck { Page(perPage: 1) { pageInfo { total } } }`

func (c *Client) Recheck(ctx context.Context) error {
	c.mu.Lock()
	c.downUntil = time.Time{}
	c.mu.Unlock()
	var out struct{}
	return c.QueryFresh(ctx, recheckQuery, nil, &out)
}
