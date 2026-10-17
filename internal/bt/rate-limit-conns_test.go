package bt

import (
	"os"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"yoru/internal/bt/internal/testutil"
)

// A busy download emptying a capped bucket doesn't shut other torrents out of peers; a zero limit does.
func TestCappedClientStillMakesConns(t *testing.T) {
	limiter := rate.NewLimiter(rate.Inf, 256<<10)
	cfg := TestingConfig(t)
	cfg.DownloadRateLimiter = limiter
	cl, err := NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	dir, mi := testutil.GreetingTestTorrent()
	defer os.RemoveAll(dir)
	tt, _, err := cl.AddTorrentSpec(TorrentSpecFromMetaInfo(mi))
	if err != nil {
		t.Fatal(err)
	}
	tt.DownloadAll()
	allowed := func() bool {
		cl.lock()
		defer cl.unlock()
		return tt.newConnsAllowed()
	}
	// Once its pieces are checked it wants data, so peers.
	for end := time.Now().Add(5 * time.Second); !allowed(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatal("the torrent never wanted peers")
		}
	}

	// What a download at the cap does to the shared bucket.
	limiter.SetLimit(1 << 20)
	limiter.ReserveN(time.Now(), limiter.Burst())
	limiter.ReserveN(time.Now(), limiter.Burst())
	if limiter.Tokens() > 0 {
		t.Fatalf("the bucket has %.0f tokens left", limiter.Tokens())
	}
	if !allowed() {
		t.Error("an empty bucket under a cap shut new connections out")
	}

	limiter.SetLimit(0)
	if allowed() {
		t.Error("a zero limit still allows connections")
	}
}
