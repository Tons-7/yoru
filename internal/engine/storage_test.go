package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"yoru/internal/bt/metainfo"
	"yoru/internal/bt/storage"
)

func TestStorageRefusesPathsOutsideTheCache(t *testing.T) {
	base := filepath.Join(t.TempDir(), "cache")
	info := &metainfo.Info{Name: "pack", PieceLength: 4, Files: []metainfo.FileInfo{
		{Length: 4, Path: []string{"..", "..", "cache2", "x.mkv"}},
	}}
	if _, err := (files{base: base}).OpenTorrent(context.Background(), info, metainfo.Hash{}); err == nil {
		t.Fatal("a path outside the cache was accepted")
	}
}

func TestCompletionFollowsTheDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ep.mkv")
	info := &metainfo.Info{PieceLength: 4, Length: 8, Name: "ep.mkv", Pieces: make([]byte, 40)}
	pieces := storage.NewMapPieceCompletion()
	ft := &fileTorrent{base: dir, info: info, pieces: pieces, files: []segment{{path: path, length: 8}}, sparse: map[string]bool{}}
	piece := filePiece{ft, info.Piece(0)}

	if c := piece.Completion(); !c.Ok || c.Complete {
		t.Errorf("no file, no record = %+v, want known incomplete", c)
	}

	os.WriteFile(path, []byte("12345678"), 0o644)
	if c := piece.Completion(); c.Ok {
		t.Errorf("bytes without a record = %+v, want unknown", c)
	}

	piece.MarkComplete()
	if c := piece.Completion(); !c.Ok || !c.Complete {
		t.Errorf("recorded and on disk = %+v, want complete", c)
	}

	os.Remove(path)
	if c := piece.Completion(); !c.Ok || c.Complete {
		t.Errorf("record outliving its file = %+v, want incomplete", c)
	}
}

func TestShortenedFileForgetsItsTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ep.mkv")
	info := &metainfo.Info{PieceLength: 4, Length: 8, Name: "ep.mkv", Pieces: make([]byte, 40)}
	pieces := storage.NewMapPieceCompletion()
	ft := &fileTorrent{base: dir, info: info, pieces: pieces, files: []segment{{path: path, length: 8}}, sparse: map[string]bool{}}
	head, tail := filePiece{ft, info.Piece(0)}, filePiece{ft, info.Piece(1)}
	os.WriteFile(path, []byte("12345678"), 0o644)
	head.MarkComplete()
	tail.MarkComplete()

	os.Truncate(path, 4)
	if c := tail.Completion(); c.Complete {
		t.Fatalf("cut-off piece = %+v, want incomplete", c)
	}
	if _, err := tail.WriteAt([]byte("x"), 3); err != nil {
		t.Fatal(err)
	}
	if c := tail.Completion(); c.Complete {
		t.Errorf("regrown over a hole = %+v, want incomplete", c)
	}
	if c := head.Completion(); !c.Ok || !c.Complete {
		t.Errorf("the intact head = %+v, want complete", c)
	}
}

func TestMissingCacheFolderKeepsRecords(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "drive")
	path := filepath.Join(dir, "ep.mkv")
	info := &metainfo.Info{PieceLength: 4, Length: 4, Name: "ep.mkv", Pieces: make([]byte, 20)}
	pieces := storage.NewMapPieceCompletion()
	ft := &fileTorrent{base: dir, info: info, pieces: pieces, files: []segment{{path: path, length: 4}}, sparse: map[string]bool{}}
	piece := filePiece{ft, info.Piece(0)}
	piece.MarkComplete()
	if c := piece.Completion(); c.Complete {
		t.Fatalf("unreachable file = %+v, want not complete for now", c)
	}
	os.MkdirAll(dir, 0o755)
	os.WriteFile(path, []byte("1234"), 0o644)
	if c := piece.Completion(); !c.Ok || !c.Complete {
		t.Errorf("drive back = %+v, want the record kept", c)
	}
}

func TestWriteAfterCloseCreatesNothing(t *testing.T) {
	dir := t.TempDir()
	info := &metainfo.Info{PieceLength: 4, Name: "Pack", Pieces: make([]byte, 20), Files: []metainfo.FileInfo{
		{Path: []string{"a.mkv"}, Length: 2}, {Path: []string{"b.mkv"}, Length: 2},
	}}
	s := files{base: dir, pieces: storage.NewMapPieceCompletion(), names: newLayout(dir)}
	impl, err := s.OpenTorrent(t.Context(), info, metainfo.Hash{1})
	if err != nil {
		t.Fatal(err)
	}
	impl.Close()
	if _, err := impl.Piece(info.Piece(0)).WriteAt([]byte("x"), 0); !errors.Is(err, errStorageClosed) {
		t.Fatalf("write after close: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Pack")); !os.IsNotExist(err) {
		t.Errorf("the pack folder came back: %v", err)
	}
}
