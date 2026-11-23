package bt

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"yoru/internal/bt/bencode"
	"yoru/internal/bt/metainfo"
	"yoru/internal/bt/storage"
)

func TestReaderRequestsUseFileOffset(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pack")
	os.MkdirAll(root, 0o755)
	os.WriteFile(filepath.Join(root, "a.mkv"), make([]byte, 100<<10), 0o644)
	os.WriteFile(filepath.Join(root, "b.mkv"), make([]byte, 200<<10), 0o644)
	info := metainfo.Info{PieceLength: 64 << 10}
	if err := info.BuildFromFilePath(root); err != nil {
		t.Fatal(err)
	}
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	mi := metainfo.MetaInfo{InfoBytes: infoBytes}

	cl := newTestingClient(t)
	cs := storage.NewFile(t.TempDir())
	t.Cleanup(func() { cs.Close() })
	tt, _ := cl.AddTorrentOpt(AddTorrentOpts{InfoHash: mi.HashInfoBytes(), InfoBytes: infoBytes,
		Storage: cs, DisableInitialPieceCheck: true})
	<-tt.GotInfo()
	files := tt.Files()
	second := files[1]
	r := second.NewReader()
	defer r.Close()

	const chunk = 16 << 10
	if _, err := r.Seek(3*chunk, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	r.ReadContext(gone, make([]byte, 1))

	cl.lock()
	got := tt.readerRequests()
	req, _ := tt.offsetRequest(second.Offset() + 3*chunk)
	want := tt.requestIndexFromRequest(req)
	cl.unlock()
	if len(got) != 1 || got[0] != want {
		t.Fatalf("reader requests = %v, want [%v] (file offset %d plus the seek)", got, want, second.Offset())
	}

	if _, err := r.Seek(0, io.SeekEnd); err != nil {
		t.Fatal(err)
	}
	r.ReadContext(gone, make([]byte, 1))
	cl.lock()
	atEnd := tt.readerRequests()
	cl.unlock()
	if len(atEnd) != 0 {
		t.Errorf("a reader at the end of its file still asks for %v", atEnd)
	}
}
