package engine

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A truncated session.json (a crash mid-write) must not lose every download: the list is rebuilt from
// the saved torrents, paused, with the files that are on disk selected, and the damaged file kept aside.
func TestDamagedSessionIsRebuiltFromTheSavedTorrents(t *testing.T) {
	seedDir := t.TempDir()
	mi, content := makePack(t, seedDir)
	session := t.TempDir()
	var buf bytes.Buffer
	mi.Write(&buf)
	hash := mi.HashInfoBytes().HexString()
	os.WriteFile(filepath.Join(session, hash+".torrent"), buf.Bytes(), 0o644)
	os.WriteFile(filepath.Join(session, "session.json"), []byte(`{"torrents":[{"info_hash":"`+hash), 0o644)
	cache := t.TempDir()
	os.MkdirAll(filepath.Join(cache, "Show Pack"), 0o755)
	os.WriteFile(filepath.Join(cache, "Show Pack", "Show - 02.mkv"), content["Show - 02.mkv"], 0o644)

	f := &fixture{cache: cache, session: session, peer: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}}
	f.start(t)
	c := ctx(t)
	live, err := f.client.Live(c)
	if err != nil || len(live) != 1 {
		t.Fatalf("live = %v %v, want the saved torrent back", live, err)
	}
	id := live[hash]
	// Paused until someone asks (WaitLive is such an ask), with only the file on disk selected: a whole
	// pack selected would make the next play fetch every episode.
	deadline := time.Now().Add(10 * time.Second)
	for {
		st, _ := f.client.Stats(c, id)
		if st.State == "paused" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("state = %s, want paused", st.State)
		}
		time.Sleep(100 * time.Millisecond)
	}
	d, _ := f.client.Details(c, id)
	for _, file := range d.Files {
		if want := file.Name == "Show - 02.mkv"; file.Included != want {
			t.Fatalf("file %s included=%v, want %v", file.Name, file.Included, want)
		}
	}
	if _, err := os.Stat(filepath.Join(session, "session.json.bad")); err != nil {
		t.Error("the damaged list was not kept aside")
	}
	if body, _ := os.ReadFile(filepath.Join(session, "session.json")); !bytes.Contains(body, []byte(hash)) {
		t.Error("the rebuilt list was not saved")
	}
}
