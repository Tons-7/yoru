package server

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"yoru/internal/config"
	"yoru/internal/library"
)

func TestShowFolderOpensOnlyYorusOwnFolders(t *testing.T) {
	var opened, revealed []string
	openFolder = func(path string) error { opened = append(opened, path); return nil }
	revealFile = func(path string) error { revealed = append(revealed, path); return nil }
	t.Cleanup(func() { openFolder, revealFile = config.OpenFolder, config.Reveal })

	h := newHarness(t, config.Config{}, nil)
	h.server.scanner = library.NewScanner(h.store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	cache := filepath.Join(t.TempDir(), "not-yet")
	h.server.cfg.CacheDir = cache

	if code, _ := h.call(t, "POST", "/api/setup/show", `{"which":"cache"}`); code != http.StatusNoContent {
		t.Fatalf("cache: %d", code)
	}
	if len(opened) != 1 || opened[0] != cache {
		t.Fatalf("opened %v, want the cache folder", opened)
	}
	if _, err := os.Stat(cache); err != nil {
		t.Errorf("a folder not made yet is made before it is opened: %v", err)
	}
	if code, _ := h.call(t, "POST", "/api/setup/show", `{"which":"config"}`); code != http.StatusNoContent || len(revealed) != 1 {
		t.Fatalf("config: %d, revealed %v", code, revealed)
	}
	for _, bad := range []string{`{"which":"C:\\Windows"}`, `{"which":""}`, `{"which":"../data"}`} {
		if code, _ := h.call(t, "POST", "/api/setup/show", bad); code != http.StatusBadRequest {
			t.Errorf("%s answered %d, want 400", bad, code)
		}
	}
	if len(opened) != 1 {
		t.Errorf("something else was opened: %v", opened)
	}

	shows := filepath.Join(t.TempDir(), "Anime")
	if err := os.MkdirAll(shows, 0o755); err != nil {
		t.Fatal(err)
	}
	if code, body := h.call(t, "POST", "/api/local/paths", fmt.Sprintf(`{"paths":[%q]}`, shows)); code != http.StatusOK {
		t.Fatalf("adding a library folder: %d %v", code, body)
	}
	if code, _ := h.call(t, "POST", "/api/setup/show", fmt.Sprintf(`{"which":"library","path":%q}`, shows)); code != http.StatusNoContent {
		t.Fatalf("a library folder: %d", code)
	}
	if len(opened) != 2 || opened[1] != shows {
		t.Fatalf("opened %v, want the library folder", opened)
	}
	if code, _ := h.call(t, "POST", "/api/setup/show", `{"which":"library","path":"C:\\Windows"}`); code != http.StatusBadRequest {
		t.Errorf("a folder that is not in the library answered %d, want 400", code)
	}
}

func TestPickingAFolderAnswersThePathOrThatNoneWasPicked(t *testing.T) {
	answer, fails := "", error(nil)
	var asked []string
	pickFolder = func(title, start string) (string, error) {
		asked = append(asked, title+"|"+start)
		return answer, fails
	}
	canPick = func() bool { return true }
	t.Cleanup(func() { pickFolder, canPick = config.PickFolder, config.CanPick })
	h := newHarness(t, config.Config{}, nil)

	if _, body := h.call(t, "GET", "/api/setup/pick", ""); body["available"] != true {
		t.Fatalf("on the host the picker is offered: %v", body)
	}
	answer = `D:\Anime`
	if code, body := h.call(t, "POST", "/api/setup/pick", `{"title":"Add a folder","start":"D:\\"}`); code != http.StatusOK || body["path"] != answer {
		t.Fatalf("picked: %d %v", code, body)
	}
	if len(asked) != 1 || asked[0] != `Add a folder|D:\` {
		t.Errorf("asked %v", asked)
	}
	answer, fails = "", config.ErrPickCancelled
	if code, body := h.call(t, "POST", "/api/setup/pick", `{}`); code != http.StatusOK || body["cancelled"] != true {
		t.Errorf("cancelled: %d %v", code, body)
	}
	fails = config.ErrPickBusy
	if code, _ := h.call(t, "POST", "/api/setup/pick", `{}`); code != http.StatusConflict {
		t.Errorf("busy: %d", code)
	}
	fails = config.ErrNoPicker
	if code, _ := h.call(t, "POST", "/api/setup/pick", `{}`); code != http.StatusNotImplemented {
		t.Errorf("no picker: %d", code)
	}
}
