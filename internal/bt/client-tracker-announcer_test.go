package bt

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"yoru/internal/bt/internal/testutil"
)

func TestReaddedTorrentAnnouncesAgain(t *testing.T) {
	events := make(chan string, 16)
	tracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		events <- r.URL.Query().Get("event")
		w.Write([]byte("d8:intervali1800e5:peers0:e"))
	}))
	defer tracker.Close()

	cfg := TestingConfig(t)
	cfg.DisableTrackers = false
	cl, err := NewClient(cfg)
	require.NoError(t, err)
	defer cl.Close()

	mi := testutil.GreetingMetaInfo()
	mi.Announce = tracker.URL + "/announce"
	mi.AnnounceList = nil
	wait := func(want string) {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for {
			select {
			case got := <-events:
				if got == want {
					return
				}
			case <-deadline:
				t.Fatalf("no %q announce", want)
			}
		}
	}

	tt, err := cl.AddTorrent(mi)
	require.NoError(t, err)
	wait("started")
	tt.Drop()
	wait("stopped")

	_, err = cl.AddTorrent(mi)
	require.NoError(t, err)
	wait("started")

	cl.lock()
	defer cl.unlock()
	require.Len(t, cl.regularTrackerAnnounceDispatcher.announceStates, 1)
}
