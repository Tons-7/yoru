package library

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"yoru/internal/store"
	"yoru/internal/torrent"
)

func TestAReplacedEpisodeLeavesThePackItSharedAndTheDisk(t *testing.T) {
	engine := newFakeEngine()
	var mu sync.Mutex
	var removed []string
	mux := http.NewServeMux()
	mux.Handle("/", engine.handler())
	mux.HandleFunc("POST /torrents/{id}/files/{file}/delete", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		removed = append(removed, r.PathValue("id")+"/"+r.PathValue("file"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	_, st := pausableQueue(t)
	ctx := context.Background()
	recordPack(t, st, otherHash, "2", "3")
	engine.ids[50] = otherHash
	if err := st.SetCacheBytes(ctx, otherHash, 2, 14, false); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordTorrent(ctx, store.TorrentRecord{InfoHash: thirdHash, EngineID: 51,
		Name: "[Quick] Show - 02 [1080p].mkv", AnimeID: 1, EpKey: "2", FilePath: "Show - 02.mkv", TotalSize: 100}); err != nil {
		t.Fatal(err)
	}

	eps, _ := st.SupersededEpisodes(ctx)
	if !slices.Contains(eps, store.EpisodeRef{AnimeID: 1, EpKey: "2"}) {
		t.Fatalf("episodes to tidy %+v; the replaced episode of a shared pack is missing", eps)
	}

	dropSuperseded(ctx, st, torrent.NewClient(srv.URL), discard(), 1, "2")
	if len(removed) != 0 {
		t.Fatalf("removed %v before the replacement was whole", removed)
	}

	if err := st.SetCacheBytes(ctx, thirdHash, 0, 100, true); err != nil {
		t.Fatal(err)
	}
	delete(engine.ids, 50)
	engine.ids[52] = thirdHash
	dropSuperseded(ctx, st, torrent.NewClient(srv.URL), discard(), 1, "2")
	if left, _ := st.SupersededFiles(ctx, 1, "2"); len(left) == 0 {
		t.Fatal("forgot the old copy while its pack was out of the engine; its bytes would stay on disk uncounted")
	}

	delete(engine.ids, 52)
	engine.ids[50] = otherHash
	dropSuperseded(ctx, st, torrent.NewClient(srv.URL), discard(), 1, "2")

	if !slices.Equal(removed, []string{"50/2"}) {
		t.Errorf("file deletes %v; want episode 2's file of the pack, alone", removed)
	}
	if slices.ContainsFunc(engine.deleted, func(h string) bool { return strings.EqualFold(h, otherHash) }) {
		t.Error("the whole pack went, with the episode it still holds")
	}
	if left, _ := st.SupersededFiles(ctx, 1, "2"); len(left) != 0 {
		t.Errorf("still remembered as superseded: %+v", left)
	}
	if rec, ok, _ := st.TorrentForEpisode(ctx, 1, "3"); !ok || !strings.EqualFold(rec.InfoHash, otherHash) {
		t.Errorf("episode 3 is %+v, want it still in the pack", rec)
	}
}
