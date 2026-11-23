package server

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"yoru/internal/store"
)

func TestBetterCopiesRoutes(t *testing.T) {
	h, _ := downloadsHarness(t)
	code, body := h.call(t, "GET", "/api/better-copies?anime=1", "")
	if items, ok := body["items"].([]any); code != http.StatusOK || !ok || len(items) != 0 {
		t.Fatalf("list answered %d: %v", code, body)
	}
	if code, body := h.call(t, "POST", "/api/better-copies/1/replace", `{"infoHash":"`+heldHash+`"}`); code != http.StatusConflict {
		t.Errorf("replacing what isn't offered answered %d: %v", code, body)
	}
	if code, body := h.call(t, "POST", "/api/better-copies/1/dismiss", `{"infoHash":"`+heldHash+`"}`); code != http.StatusOK || body["dismissed"] != true {
		t.Errorf("dismiss answered %d: %v", code, body)
	}
}

func TestRemovingAnEpisodeDropsItFromTheLine(t *testing.T) {
	h, _ := downloadsHarness(t)
	ctx := context.Background()
	if err := h.store.RecordTorrent(ctx, store.TorrentRecord{InfoHash: heldHash, EngineID: 1, Name: "Show - 01.mkv",
		AnimeID: 1, EpKey: "2", FileIndex: 0, FilePath: "Show - 02.mkv", TotalSize: 100}); err != nil {
		t.Fatal(err)
	}
	if code, body := h.call(t, "DELETE", "/api/downloads/"+heldHash+"/files/0", ""); code != http.StatusOK {
		t.Fatalf("remove answered %d: %v", code, body)
	}
	keys, _ := queuedKeys(t, h)
	if slices.Contains(keys, "2") {
		t.Errorf("queue %v still holds the removed episode", keys)
	}
	if !slices.Contains(keys, "3") {
		t.Errorf("queue %v lost another episode", keys)
	}
}
