package anilist

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

const (
	cacheTTL     = 10 * time.Minute
	cacheEntries = 250
)

type cached struct {
	body    []byte
	expires time.Time
}

type responseCache struct {
	mu      sync.Mutex
	entries map[string]cached

	inflight map[string]*flight
	failed   map[string]bool
}

func newResponseCache() *responseCache {
	return &responseCache{
		entries:  map[string]cached{},
		inflight: map[string]*flight{},
	}
}

func isMutation(query string) bool {
	return strings.HasPrefix(strings.TrimSpace(query), "mutation")
}

func cacheKey(body []byte, token string) string {
	sum := sha256.New()
	sum.Write(body)
	sum.Write([]byte{0})
	sum.Write([]byte(token))
	return hex.EncodeToString(sum.Sum(nil))
}

func (c *responseCache) get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok || time.Now().After(entry.expires) {
		return nil, false
	}
	return entry.body, true
}

func (c *responseCache) put(key string, body []byte) {
	c.putUntil(key, body, time.Now().Add(cacheTTL))
}

func (c *responseCache) noteFailed(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.failed == nil {
		c.failed = map[string]bool{}
	}
	for old := range c.failed {
		if len(c.failed) <= cacheEntries {
			break
		}
		delete(c.failed, old)
	}
	c.failed[key] = true
}

func (c *responseCache) refused(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.failed[key]
}

func (c *responseCache) putUntil(key string, body []byte, expires time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= cacheEntries {
		now := time.Now()
		for k, e := range c.entries {
			if now.After(e.expires) {
				delete(c.entries, k)
			}
		}
	}
	for len(c.entries) >= cacheEntries {
		var soonest string
		var at time.Time
		for k, e := range c.entries {
			if soonest == "" || e.expires.Before(at) {
				soonest, at = k, e.expires
			}
		}
		delete(c.entries, soonest)
	}
	delete(c.failed, key)
	c.entries[key] = cached{body: body, expires: expires}
}

type flight struct {
	wait chan struct{}
	err  error
}

func (c *responseCache) enter(key string) (*flight, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if f, busy := c.inflight[key]; busy {
		return f, false
	}
	f := &flight{wait: make(chan struct{})}
	c.inflight[key] = f
	return f, true
}

func (c *responseCache) done(key string, f *flight, err error) {
	c.mu.Lock()
	delete(c.inflight, key)
	f.err = err
	c.mu.Unlock()
	close(f.wait)
}

func (c *responseCache) failure(f *flight) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return f.err
}
