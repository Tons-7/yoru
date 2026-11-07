package library

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"

	"yoru/internal/db"
	"yoru/internal/store"
	"yoru/internal/torrent"
)

func TestAReplaceByHashTakesTheEntrysOwnSeasonFromAPack(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := conn.Migrate(); err != nil {
		t.Fatal(err)
	}
	st := store.New(conn)
	episodes := 12
	ctx := context.Background()
	if _, err := st.ImportList(ctx,
		[]store.Anime{{ID: 7, Romaji: "Sousou no Frieren 2nd Season", Synonyms: "[]", Genres: "[]", Episodes: &episodes}},
		nil, store.ImportMerge); err != nil {
		t.Fatal(err)
	}
	finder := NewFinder(st, fixedIndexer{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	scope := finder.PackScope(ctx, 7, 3, 0)
	if scope.Season != 2 {
		t.Fatalf("season %d, want 2", scope.Season)
	}
	if !slices.Contains(scope.Others, "movie") {
		t.Errorf("others %v, want the words that mark a film", scope.Others)
	}

	files := []torrent.File{
		{Name: "Frieren/Season 1/Frieren - S01E03.mkv", Length: 1 << 30},
		{Name: "Frieren/Season 2/Frieren - S02E03.mkv", Length: 1 << 30},
		{Name: "Frieren/Movie/Frieren - Movie 03.mkv", Length: 3 << 30},
	}
	numbers := fileNumbers(ctx, st, 7, 3)
	f, _, ok := torrent.PickInPack(files, 3, numbers, scope)
	if !ok || f.Name != files[1].Name {
		t.Fatalf("picked %q (%v), want the season 2 episode", f.Name, ok)
	}
	if f, _, ok := torrent.PickInPack(files, 3, numbers, torrent.Scope{Season: 1, Others: scope.Others}); !ok || f.Name != files[0].Name {
		t.Errorf("with season 1 picked %q (%v)", f.Name, ok)
	}
}
