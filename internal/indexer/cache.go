package indexer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const searchTTL = 3 * time.Minute

type cachedSearch struct {
	results []Torrent
	expires time.Time
}

type Cached struct {
	source Source

	mu       sync.Mutex
	entries  map[string]cachedSearch
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

const sharedSearchTimeout = 2 * time.Minute

func (c *Cached) store(key string, results []Torrent) {
	c.mu.Lock()
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
