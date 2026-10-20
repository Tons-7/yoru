package indexer

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type countingSource struct {
	calls int
	err   error
}

func (c *countingSource) Name() string { return "counting" }

func (c *countingSource) Search(context.Context, Query) ([]Torrent, error) {
	c.calls++
	if c.err != nil {
		return nil, c.err
	}
	return []Torrent{{Title: "result", InfoHash: "abc"}}, nil
}

// Opening the sources list and then pressing play searched twice, and every
// failed release searched again. On a slow line each round is seconds.
func TestRepeatedSearchAsksTheSourceOnce(t *testing.T) {
	inner := &countingSource{}
	c := NewCached(inner)
	ctx := context.Background()

	for range 4 {
		got, err := c.Search(ctx, Query{Text: "Frieren 01"})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d results, want 1", len(got))
		}
	}
	if inner.calls != 1 {
		t.Errorf("%d searches, want 1", inner.calls)
	}

	if _, err := c.Search(ctx, Query{Text: "Frieren 02"}); err != nil {
		t.Fatal(err)
	}
	if inner.calls != 2 {
		t.Errorf("%d searches after a different query, want 2", inner.calls)
	}
}

// A failed search is usually a timeout, and repeating it is the whole point.
func TestFailuresAreNotCached(t *testing.T) {
	inner := &countingSource{err: errors.New("timeout")}
	c := NewCached(inner)

	for range 3 {
		if _, err := c.Search(context.Background(), Query{Text: "Frieren 01"}); err == nil {
			t.Fatal("expected the error to be reported")
		}
	}
	if inner.calls != 3 {
		t.Errorf("%d searches, want 3: a failure must be retried", inner.calls)
	}
}

// The same query asked twice at once goes out once; the second caller takes the first's answer.
func TestConcurrentIdenticalSearchesShareOneRequest(t *testing.T) {
	inner := &slowSource{release: make(chan struct{})}
	c := NewCached(inner)
	ctx := context.Background()

	done := make(chan struct{}, 2)
	for range 2 {
		go func() {
			c.Search(ctx, Query{Text: "Frieren 01"})
			done <- struct{}{}
		}()
	}
	for inner.started.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	close(inner.release)
	<-done
	<-done
	if n := inner.calls.Load(); n != 1 {
		t.Fatalf("%d searches went out, want 1", n)
	}
}

// The first asker leaving (the sources list closed) doesn't fail Play waiting on the same search.
func TestLeavingAskerDoesNotFailTheOthers(t *testing.T) {
	inner := &slowSource{release: make(chan struct{}), honour: true}
	c := NewCached(inner)
	first, cancel := context.WithCancel(context.Background())
	firstErr := make(chan error, 1)
	go func() {
		_, err := c.Search(first, Query{Text: "Frieren 01"})
		firstErr <- err
	}()
	for inner.started.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	second := make(chan error, 1)
	go func() {
		got, err := c.Search(context.Background(), Query{Text: "Frieren 01"})
		if err == nil && len(got) != 1 {
			err = errors.New("no results")
		}
		second <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := <-firstErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("the leaving asker got %v, want its own cancellation", err)
	}
	close(inner.release)
	if err := <-second; err != nil {
		t.Fatalf("the waiting asker failed: %v", err)
	}
	if n := inner.calls.Load(); n != 1 {
		t.Fatalf("%d searches went out, want 1", n)
	}
}

type slowSource struct {
	honour  bool // gives up when its context ends, as the real indexers do
	release chan struct{}
	started atomic.Int32
	calls   atomic.Int32
}

func (s *slowSource) Name() string { return "slow" }
func (s *slowSource) Search(ctx context.Context, _ Query) ([]Torrent, error) {
	s.calls.Add(1)
	s.started.Add(1)
	done := ctx.Done()
	if !s.honour {
		done = nil
	}
	select {
	case <-s.release:
	case <-done:
		return nil, ctx.Err()
	}
	return []Torrent{{Title: "result", InfoHash: "abc"}}, nil
}
