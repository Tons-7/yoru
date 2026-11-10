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

	inflight map[string]chan struct{}
	failed   map[string]bool
}

func newResponseCache() *responseCache {
	return &responseCache{
		entries:  map[string]cached{},
		inflight: map[string]chan struct{}{},
	}
}

func isMutation(query string) bool {
	return strings.HasPrefix(strings.TrimSpace(query), "mutation")
}

func cacheKey(query string, body []byte, token string) string {
	sum := sha256.New()
	sum.Write([]byte(query))
	sum.Write([]byte{0})
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
	if c.failed == nil || len(c.failed) > cacheEntries {
		c.failed = map[string]bool{}
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

		for k := range c.entries {
			if len(c.entries) < cacheEntries {
				break
			}
			delete(c.entries, k)
		}
	}
	delete(c.failed, key)
	c.entries[key] = cached{body: body, expires: expires}
}

func (c *responseCache) enter(key string) (chan struct{}, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if wait, busy := c.inflight[key]; busy {
		return wait, false
	}
	wait := make(chan struct{})
	c.inflight[key] = wait
	return wait, true
}

func (c *responseCache) done(key string, wait chan struct{}) {
	c.mu.Lock()
	delete(c.inflight, key)
	c.mu.Unlock()
	close(wait)
}
