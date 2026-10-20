package transcode

import (
	"bytes"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeParts writes files of the given sizes with distinct bytes, returning their paths and their bytes joined.
func writeParts(t *testing.T, dir, prefix string, sizes ...int) ([]string, []byte) {
	t.Helper()
	var paths []string
	var whole []byte
	for i, size := range sizes {
		data := make([]byte, size)
		for j := range data {
			data[j] = byte(i*31 + j*7)
		}
		p := filepath.Join(dir, fmt.Sprintf("%s_%d.mp4", prefix, i))
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
		whole = append(whole, data...)
	}
	return paths, whole
}

// Parts read as one file: whole, from any offset, and by ranges over HTTP, a part of no bytes included.
func TestSegmentReaderReadsPartsAsOne(t *testing.T) {
	paths, whole := writeParts(t, t.TempDir(), "p", 70_001, 0, 128_000, 3)
	r, err := openParts(paths, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if r.Size() != int64(len(whole)) {
		t.Fatalf("size %d, want %d", r.Size(), len(whole))
	}
	got, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(got, whole) {
		t.Fatalf("whole read differs (err %v, %d bytes)", err, len(got))
	}

	rng := rand.New(rand.NewPCG(1, 2))
	for range 1000 {
		off := rng.Int64N(int64(len(whole)) + 10)
		if _, err := r.Seek(off, io.SeekStart); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, rng.IntN(90_000)+1)
		n, err := io.ReadFull(r, buf)
		want := whole[min(off, int64(len(whole))):min(off+int64(len(buf)), int64(len(whole)))]
		if !bytes.Equal(buf[:n], want) || (n < len(buf) && err == nil) {
			t.Fatalf("at %d for %d: got %d bytes (err %v), want %d", off, len(buf), n, err, len(want))
		}
	}
	if pos, _ := r.Seek(-5, io.SeekEnd); pos != int64(len(whole))-5 {
		t.Errorf("seek from the end landed at %d", pos)
	}
	if _, err := r.Seek(-1, io.SeekStart); err == nil {
		t.Error("a seek before the start should fail")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		seg, err := openParts(paths, time.Time{})
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer seg.Close()
		http.ServeContent(w, req, "", seg.Modified, seg)
	}))
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Range", "bytes=69990-70020")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusPartialContent || !bytes.Equal(body, whole[69990:70021]) {
		t.Errorf("range across parts: %s, %d bytes", res.Status, len(body))
	}
	res, err = http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.ContentLength != int64(len(whole)) || !bytes.Equal(body, whole) {
		t.Errorf("whole over HTTP: length %d, %d bytes", res.ContentLength, len(body))
	}
}

// A reader holding a segment's parts reads them to the end though they are removed, as a replacement does.
func TestSegmentReaderOutlivesRemovedParts(t *testing.T) {
	paths, whole := writeParts(t, t.TempDir(), "p", 50_000, 60_000)
	r, err := openParts(paths, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	head := make([]byte, 10)
	if _, err := io.ReadFull(r, head); err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		if err := os.Remove(p); err != nil {
			t.Fatalf("removing a part being read: %v", err)
		}
	}
	rest, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(append(head, rest...), whole) {
		t.Fatalf("read after removal differs (err %v)", err)
	}
}

// Replacing a segment leaves an open reader on the old parts, gives the next one the new; dropping clears it.
func TestPublishReplacesWithoutDisturbingAReader(t *testing.T) {
	dir := t.TempDir()
	s := &Session{dir: dir, starts: []float64{0, 6}, Segments: 2, Info: &MediaInfo{Duration: 12}}
	a, b := &run{id: 1}, &run{id: 2}
	oldParts, oldBytes := writeParts(t, dir, "p1", 4000, 5000)
	s.publish(a, 1, oldParts)
	if !s.segmentDone(1) || s.segmentDone(0) {
		t.Fatal("only segment 1 is published")
	}
	s.mu.Lock()
	byA := s.madeByLocked(a, 1)
	s.mu.Unlock()
	if !byA {
		t.Error("segment 1 should count as pass a's")
	}

	reading, err := s.OpenSegment(1)
	if err != nil {
		t.Fatal(err)
	}
	newParts, newBytes := writeParts(t, dir, "p2", 3000)
	s.publish(b, 1, newParts)
	for _, p := range oldParts {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("a replaced part stays on disk: %s", filepath.Base(p))
		}
	}
	got, err := io.ReadAll(reading)
	reading.Close()
	if err != nil || !bytes.Equal(got, oldBytes) {
		t.Errorf("the reader open before the replacement got %d bytes (err %v), want the old %d", len(got), err, len(oldBytes))
	}
	if fresh := segmentBytesOf(t, s, 1); !bytes.Equal(fresh, newBytes) {
		t.Errorf("a reader after the replacement got %d bytes, want the new %d", len(fresh), len(newBytes))
	}
	s.mu.Lock()
	byB := s.madeByLocked(b, 1) && !s.madeByLocked(a, 1)
	s.mu.Unlock()
	if !byB {
		t.Error("segment 1 should now count as pass b's alone")
	}

	s.dropSegments()
	if s.segmentDone(1) {
		t.Error("dropped segments are gone")
	}
	if _, err := os.Stat(newParts[0]); !os.IsNotExist(err) {
		t.Error("a dropped segment's part stays on disk")
	}
	if _, err := s.OpenSegment(1); err == nil {
		t.Error("opening a dropped segment should fail")
	}
}

func segmentBytesOf(t *testing.T, s *Session, n int) []byte { return segmentBytes(t, s, n) }

// A player whose connection drops mid-segment gets it whole on asking again, and the cut send holds no file
// open (on Windows a leaked handle would keep the session's folder from going).
func TestSegmentServingSurvivesACutConnection(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "session")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	s := &Session{dir: dir, starts: []float64{0}, Segments: 1, Info: &MediaInfo{Duration: 6}}
	parts, whole := writeParts(t, dir, "p1", 700_000, 900_000)
	s.publish(&run{id: 1}, 0, parts)

	served := make(chan struct{}, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		defer func() { served <- struct{}{} }()
		seg, err := s.OpenSegment(0)
		if err != nil {
			http.Error(w, err.Error(), http.StatusGatewayTimeout)
			return
		}
		defer seg.Close()
		http.ServeContent(w, req, "", seg.Modified, seg)
	}))
	defer srv.Close()

	res, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(res.Body, make([]byte, 1000)); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	srv.CloseClientConnections()
	<-served

	res, err = http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	<-served
	if err != nil || !bytes.Equal(body, whole) {
		t.Fatalf("asked again after the cut: %d bytes (err %v), want %d", len(body), err, len(whole))
	}
	srv.Close()
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("a send left a part open: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("the session's folder outlived its sends")
	}
}

// While a segment is replaced over and over, every read gets one whole version, never a mix or an error.
func TestSegmentReadsAreWholeUnderReplacement(t *testing.T) {
	dir := t.TempDir()
	s := &Session{dir: dir, starts: []float64{0}, Segments: 1, Info: &MediaInfo{Duration: 6}}
	versions := map[string]bool{}
	var parts [][]string
	for v := range 40 {
		p, whole := writeParts(t, dir, fmt.Sprintf("v%d", v), 9000+v*37, 4000+v*11)
		parts = append(parts, p)
		versions[string(whole)] = true
	}
	s.publish(&run{id: 0}, 0, parts[0])

	done := make(chan struct{})
	errs := make(chan error, 4)
	for range 4 {
		go func() {
			for reads := 0; ; reads++ {
				select {
				case <-done:
					errs <- nil
					return
				default:
				}
				b, err := readSegment(s, 0)
				if err != nil || !versions[string(b)] {
					errs <- fmt.Errorf("read %d: %d bytes, err %v, a whole version: %v", reads, len(b), err, versions[string(b)])
					return
				}
			}
		}()
	}
	for v := 1; v < len(parts); v++ {
		s.publish(&run{id: v}, 0, parts[v])
		time.Sleep(2 * time.Millisecond)
	}
	close(done)
	for range 4 {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
}
