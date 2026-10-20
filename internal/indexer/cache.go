package indexer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// A search hits every indexer with six title variants and is repeated across
// listing, play and retries; short enough to still find a minutes-old release.
const searchTTL = 3 * time.Minute

type cachedSearch struct {
	results []Torrent
	expires time.Time
}

// Cached wraps a source with a short-lived result cache. Errors are never
// cached: a failed search is usually a timeout, and repeating it is the point.
type Cached struct {
	source Source

	mu      sync.Mutex
	entries map[string]cachedSearch
	// inflight: the same query asked while one is already out waits for that answer.
	inflight singleflight.Group
}

func NewCached(source Source) *Cached {
	return &Cached{source: source, entries: map[string]cachedSearch{}}
}

func (c *Cached) Name() string { return c.source.Name() }

func (c *Cached) Search(ctx context.Context, q Query) ([]Torrent, error) {
	key := fmt.Sprintf("%s\x00%s\x00%d\x00%s\x00%d", q.Text, q.Category, q.Filter, q.Uploader, q.Limit)

	c.mu.Lock()
	entry, ok := c.entries[key]
	if ok && time.Now().After(entry.expires) {
		delete(c.entries, key)
		ok = false
	}
	c.mu.Unlock()
	if ok {
		return entry.results, nil
	}

	// Detached: the first asker leaving (a closed sources list) must not fail the others waiting on its answer.
	ch := c.inflight.DoChan(key, func() (any, error) {
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sharedSearchTimeout)
		defer cancel()
		results, err := c.source.Search(sctx, q)
		if err == nil {
			c.store(key, results)
		}
		return results, err
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		results, _ := r.Val.([]Torrent)
		return results, r.Err
	}
}

// Bounds a shared search nobody may still be waiting for.
const sharedSearchTimeout = 45 * time.Second

func (c *Cached) store(key string, results []Torrent) {
	c.mu.Lock()
	// A long session browsing many shows would otherwise grow this forever.
	if len(c.entries) > 500 {
		now := time.Now()
		for k, e := range c.entries {
			if now.After(e.expires) {
				delete(c.entries, k)
			}
		}
	}
	c.entries[key] = cachedSearch{results: results, expires: time.Now().Add(searchTTL)}
	c.mu.Unlock()
}
