package store

import (
	"context"
	"path/filepath"
	"testing"

	"yoru/internal/db"
)

func TestAPackKeptAsAWholeKeepsEveryEpisode(t *testing.T) {
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	if err := conn.Migrate(); err != nil {
		t.Fatal(err)
	}
	s := New(conn)
	ctx := context.Background()
	const hash = "abababababababababababababababababababab"
	if err := s.TrackTorrent(ctx, hash, 3, "[G] Show (01-02)"); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureAnime(ctx, 1); err != nil {
		t.Fatal(err)
	}
	for i, ep := range []string{"1", "2"} {
		if _, err := s.w.ExecContext(ctx, `
			INSERT INTO torrent_file (info_hash, file_index, path, size_bytes, anime_id, ep_key, selected) VALUES (?,?,?,100,1,?,1)`,
			hash, i, "Show - 0"+ep+".mkv", ep); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.w.ExecContext(ctx, `UPDATE cache_entry SET kept = 1, complete = 1 WHERE info_hash = ? AND file_index = ?`,
		hash, WholeTorrent); err != nil {
		t.Fatal(err)
	}

	parts, err := s.DownloadParts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := parts[hash]
	if len(got) != 2 || !got[0].Kept || !got[1].Kept || !got[0].Complete {
		t.Fatalf("parts %+v; a whole-torrent entry speaks for every episode", got)
	}
}

func TestOneTorrentForTwoEntriesNamesEachEpisodesOwnShow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	const hash = "cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd"
	if _, err := s.ImportList(ctx, []Anime{
		{ID: 143202, Romaji: "Princess Principal: Crown Handler - Chapter 2: Revealing Reviews", Synonyms: "[]", Genres: "[]"},
		{ID: 166977, Romaji: "Princess Principal: Crown Handler - Chapter 3: Cost for Custom Cars", Synonyms: "[]", Genres: "[]"},
	}, nil, ImportMerge); err != nil {
		t.Fatal(err)
	}
	if err := s.TrackTorrent(ctx, hash, 3, "[Anime-AV1] Princess Principal - Crown Handler (Movies + Specials)"); err != nil {
		t.Fatal(err)
	}
	for i, id := range []int{143202, 166977} {
		if _, err := s.w.ExecContext(ctx, `
			INSERT INTO torrent_file (info_hash, file_index, path, size_bytes, anime_id, ep_key, selected) VALUES (?,?,?,100,?,'1',1)`,
			hash, i, "special.mkv", id); err != nil {
			t.Fatal(err)
		}
	}

	parts, err := s.DownloadParts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := parts[hash]
	if len(got) != 2 {
		t.Fatalf("parts %+v; want one for each entry's episode 1", got)
	}
	for i, id := range []int{143202, 166977} {
		if got[i].AnimeID != id || got[i].Title == "" {
			t.Errorf("part %d = %+v; want entry %d with its own title", i, got[i], id)
		}
	}
	if got[0].Title == got[1].Title {
		t.Errorf("both parts are called %q", got[0].Title)
	}
}
