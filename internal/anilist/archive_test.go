package anilist

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type memArchive struct {
	mu    sync.Mutex
	saved map[string][]byte
	at    time.Time
	wrote chan struct{}
}

func newMemArchive() *memArchive {
	return &memArchive{saved: map[string][]byte{}, at: time.Unix(1_700_000_000, 0), wrote: make(chan struct{}, 8)}
}

func (m *memArchive) LoadAnswer(_ context.Context, key string) ([]byte, time.Time, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.saved[key]
	return b, m.at, ok
}

func (m *memArchive) SaveAnswer(_ context.Context, key string, body []byte) error {
	m.mu.Lock()
	m.saved[key] = body
	m.mu.Unlock()
	m.wrote <- struct{}{}
	return nil
}

func TestSavedAnswerStandsInWhileAniListIsDown(t *testing.T) {
	var down atomic.Bool
	var calls atomic.Int32
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		if down.Load() {
			w.Write([]byte(`{"errors":[{"message":"Internal Server Error","status":500}]}`))
			return
		}
		w.Write([]byte(`{"data":{"n":1}}`))
	})
	c.cache = newResponseCache()
	archive := newMemArchive()
	c.SetArchive(archive)

	page, served := WithServed(context.Background())
	var out struct{ N int }
	if err := c.Query(page, "query { n }", nil, &out); err != nil || out.N != 1 {
		t.Fatalf("live query: %v %+v", err, out)
	}
	select {
	case <-archive.wrote:
	case <-time.After(2 * time.Second):
		t.Fatal("a live answer to a page was not saved")
	}
	if live, saved := served.Result(); !live || !saved.IsZero() {
		t.Fatalf("live answer reported as live=%v saved=%v", live, saved)
	}

	down.Store(true)
	c.cache = newResponseCache()

	if err := c.Query(context.Background(), "query { n }", nil, &out); err == nil {
		t.Fatal("background work must see the failure, not a saved answer")
	}

	page, served = WithServed(context.Background())
	out.N = 0
	if err := c.Query(page, "query { n }", nil, &out); err != nil || out.N != 1 {
		t.Fatalf("page during outage: %v %+v", err, out)
	}
	if _, saved := served.Result(); !saved.Equal(archive.at) || !UsedSaved(page) {
		t.Fatalf("saved answer not reported: %v", saved)
	}

	before := calls.Load()
	page, _ = WithServed(context.Background())
	if err := c.Query(page, "query { n }", nil, &out); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != before {
		t.Error("asked AniList again within the down window")
	}
}

func TestRejectionIsNotReplacedBySavedAnswer(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"errors":[{"message":"Not Found.","status":404}]}`))
	})
	archive := newMemArchive()
	c.SetArchive(archive)
	body := []byte(`{"query":"query { n }","variables":null}`)
	archive.saved[archiveKey(body, false)] = []byte(`{"n":7}`)

	page, _ := WithServed(context.Background())
	var out struct{ N int }
	if err := c.Query(page, "query { n }", nil, &out); err == nil {
		t.Fatalf("a 404 was answered from the archive: %+v", out)
	}
	if UsedSaved(page) {
		t.Error("marked saved without using it")
	}
}

func TestRecheckAsksDespiteTheDownWindow(t *testing.T) {
	asked := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		asked++
		io.WriteString(w, `{"data":{"Page":{"pageInfo":{"total":1}}}}`)
	})
	c.markDown()

	if err := c.Recheck(context.Background()); err != nil {
		t.Fatal(err)
	}
	if asked != 1 {
		t.Fatalf("%d requests, want the one live question", asked)
	}
	if c.isDown() {
		t.Error("a live answer left the client marked down")
	}
}

func TestASavedAnswerIsShownAtOnceAndRefreshedBehind(t *testing.T) {
	var calls, n atomic.Int32
	n.Store(1)
	hold := make(chan struct{})
	var held atomic.Bool
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		if held.Load() {
			<-hold
		}
		fmt.Fprintf(w, `{"data":{"n":%d}}`, n.Load())
	})
	c.cache = newResponseCache()
	archive := newMemArchive()
	c.SetArchive(archive)
	ask := func() (int, *Served, error) {
		page, served := WithServed(context.Background())
		var out struct{ N int }
		err := c.Query(page, "query { n }", nil, &out)
		return out.N, served, err
	}

	if got, served, err := ask(); err != nil || got != 1 || served.Refreshing() {
		t.Fatalf("first, live: %d %v", got, err)
	}
	<-archive.wrote

	c.cache = newResponseCache()
	archive.mu.Lock()
	archive.at = time.Now().Add(-time.Minute)
	archive.mu.Unlock()
	before := calls.Load()
	got, served, err := ask()
	if live, saved := served.Result(); err != nil || got != 1 || !live || !saved.IsZero() || served.Refreshing() || calls.Load() != before {
		t.Fatalf("a minute-old saved answer after a restart: %d %v, asked AniList %d more times", got, err, calls.Load()-before)
	}

	c.cache = newResponseCache()
	archive.mu.Lock()
	archive.at = time.Now().Add(-time.Hour)
	archive.mu.Unlock()
	n.Store(2)
	held.Store(true)
	started := time.Now()
	page, served := WithServed(context.Background())
	var out struct{ N int }
	if err := c.Query(page, "query { n }", nil, &out); err != nil || out.N != 1 {
		t.Fatalf("an hour-old saved answer: %d %v; want the old one at once", out.N, err)
	}
	if time.Since(started) > time.Second || !served.Refreshing() || !UsedSaved(page) {
		t.Fatalf("took %v, refreshing %v, used saved %v; want at once, marked as refreshing and not to be written back",
			time.Since(started), served.Refreshing(), UsedSaved(page))
	}
	if got, again, _ := ask(); got != 1 || !again.Refreshing() {
		t.Fatalf("asked again while the refresh runs: %d, refreshing %v", got, again.Refreshing())
	}

	var job struct{ N int }
	done := make(chan error, 1)
	go func() { done <- c.Query(context.Background(), "query { n }", nil, &job) }()
	select {
	case <-done:
		t.Fatal("background work was answered before AniList was")
	case <-time.After(100 * time.Millisecond):
	}
	held.Store(false)
	close(hold)
	if err := <-done; err != nil || job.N != 2 {
		t.Fatalf("background work: %d %v; want AniList's own answer", job.N, err)
	}
	<-archive.wrote
	asked := calls.Load()
	if got, fresh, err := ask(); err != nil || got != 2 || fresh.Refreshing() || UsedSaved(context.Background()) {
		t.Fatalf("after the refresh: %d %v, refreshing %v", got, err, fresh.Refreshing())
	}
	if calls.Load() != asked {
		t.Error("the refreshed answer cost another request")
	}
}

func TestARefusedRefreshDoesNotKeepTheOldAnswerForever(t *testing.T) {
	var refuse atomic.Bool
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		if refuse.Load() {
			w.Write([]byte(`{"errors":[{"message":"Not Found.","status":404}]}`))
			return
		}
		w.Write([]byte(`{"data":{"n":1}}`))
	})
	c.cache = newResponseCache()
	archive := newMemArchive()
	c.SetArchive(archive)
	var out struct{ N int }
	page, _ := WithServed(context.Background())
	if err := c.Query(page, "query { n }", nil, &out); err != nil {
		t.Fatal(err)
	}
	<-archive.wrote

	c.cache = newResponseCache()
	refuse.Store(true)
	page, served := WithServed(context.Background())
	if err := c.Query(page, "query { n }", nil, &out); err != nil || !served.Refreshing() {
		t.Fatalf("the old answer first: %v, refreshing %v", err, served.Refreshing())
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		page, served = WithServed(context.Background())
		err := c.Query(page, "query { n }", nil, &out)
		if err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("AniList's refusal never reached the page; the old answer kept being shown")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestWhoIsSignedInIsNeverTakenFromASavedAnswer(t *testing.T) {
	var name atomic.Value
	name.Store("first")
	var down atomic.Bool
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		if down.Load() {
			w.Write([]byte(`{"errors":[{"message":"Internal Server Error","status":500}]}`))
			return
		}
		fmt.Fprintf(w, `{"data":{"Viewer":{"id":1,"name":%q}}}`, name.Load())
	})
	c.cache = newResponseCache()
	archive := newMemArchive()
	c.SetArchive(archive)
	c.SetToken("one")

	page, _ := WithServed(context.Background())
	if v, err := c.Viewer(page); err != nil || v.Name != "first" {
		t.Fatalf("first sign-in: %+v %v", v, err)
	}
	select {
	case <-archive.wrote:
		t.Fatal("the account's own answer was saved for later")
	case <-time.After(150 * time.Millisecond):
	}

	archive.mu.Lock()
	archive.saved[archiveKey(mustBody(t, viewerQuery), true)] = []byte(`{"Viewer":{"id":1,"name":"first"}}`)
	archive.mu.Unlock()
	c.cache = newResponseCache()
	c.SetToken("two")
	name.Store("second")
	page, served := WithServed(context.Background())
	if v, err := c.Viewer(page); err != nil || v.Name != "second" || served.Refreshing() {
		t.Fatalf("another account signing in got %+v (%v); want its own name from AniList", v, err)
	}

	c.cache = newResponseCache()
	down.Store(true)
	page, _ = WithServed(context.Background())
	if v, err := c.Viewer(page); err == nil {
		t.Fatalf("with AniList down, signing in answered %+v from a saved copy", v)
	}
}

func mustBody(t *testing.T, query string) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{"query": query, "variables": nil})
	if err != nil {
		t.Fatal(err)
	}
	return body
}
