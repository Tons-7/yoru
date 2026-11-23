package engine

import (
	"bytes"
	"errors"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestWriteErrorPausesThenRetries(t *testing.T) {
	f := newFixture(t, slowSeed)
	c := ctx(t)
	in, err := f.client.Inspect(c, f.magnet)
	if err != nil {
		t.Fatal(err)
	}
	added, err := f.client.Add(c, f.magnet, in.Details.Files[2])
	if err != nil {
		t.Fatal(err)
	}
	en, err := f.engine.entryByID(added.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.engine.writeFailed(en, errors.New("disk full"))
	if s, _ := f.client.Stats(c, added.ID); !strings.Contains(s.Warning, "disk full") || s.Error != "" {
		t.Fatalf("after a failed write: %+v", s)
	}
	f.engine.retryWrites(en, time.Now())
	if s, _ := f.client.Stats(c, added.ID); s.Warning == "" {
		t.Fatal("retried before the wait")
	}
	f.engine.retryWrites(en, time.Now().Add(writeRetry))
	if s, _ := f.client.Stats(c, added.ID); s.Warning != "" {
		t.Fatalf("still stopped after the wait: %+v", s)
	}
	if err := f.client.Await(c, added.ID, 20*time.Second); err != nil {
		var status bytes.Buffer
		f.engine.cl.WriteStatus(&status)
		t.Fatalf("did not finish after the retry: %v\n%s", err, status.String())
	}
}

func TestPausedDownloadStaysPausedOnRetry(t *testing.T) {
	f := newFixture(t, slowSeed)
	c := ctx(t)
	in, _ := f.client.Inspect(c, f.magnet)
	added, err := f.client.Add(c, f.magnet, in.Details.Files[2])
	if err != nil {
		t.Fatal(err)
	}
	en, _ := f.engine.entryByID(added.ID)
	f.engine.writeFailed(en, errors.New("disk full"))
	if err := f.client.Pause(c, added.ID); err != nil {
		t.Fatal(err)
	}
	f.engine.retryWrites(en, time.Now().Add(writeRetry))
	if s, _ := f.client.Stats(c, added.ID); s.State != "paused" {
		t.Fatalf("a paused download resumed on the write retry: %q", s.State)
	}
}

func TestBadFileNameFailsTheDownload(t *testing.T) {
	f := newFixture(t, slowSeed)
	c := ctx(t)
	in, _ := f.client.Inspect(c, f.magnet)
	added, err := f.client.Add(c, f.magnet, in.Details.Files[2])
	if err != nil {
		t.Fatal(err)
	}
	en, _ := f.engine.entryByID(added.ID)
	bad := error(syscall.ENAMETOOLONG)
	if runtime.GOOS == "windows" {
		bad = syscall.Errno(206)
	}
	f.engine.writeFailed(en, bad)
	if s, _ := f.client.Stats(c, added.ID); s.Error == "" {
		t.Fatalf("a name the drive refuses is not an error: %+v", s)
	}
	f.engine.writeFailed(en, errStorageClosed)
	if s, _ := f.client.Stats(c, added.ID); s.Warning != "" {
		t.Errorf("a write after close was reported: %+v", s)
	}
}
