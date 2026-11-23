package store

import (
	"context"
	"testing"
)

func rewatchEntry(t *testing.T, s *Store, animeID int) (status string, progress, repeat int, dirty int) {
	t.Helper()
	err := s.r.QueryRow(
		`SELECT coalesce(status,''), progress, repeat_count, dirty FROM list_entry WHERE anime_id = ?`,
		animeID).Scan(&status, &progress, &repeat, &dirty)
	if err != nil {
		t.Fatal(err)
	}
	return
}

func TestRewatchStartsOver(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedCatalogue(t, s, 7, "FINISHED", 12)

	for n := 1; n <= 12; n++ {
		if _, err := s.MarkWatched(ctx, 7, n); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.w.Exec(`INSERT INTO playback (anime_id, ep_key, position_s, watched, last_played_at)
		VALUES (7, '3', 1400, 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if status, progress, _, _ := rewatchEntry(t, s, 7); status != "COMPLETED" || progress != 12 {
		t.Fatalf("first watch: %s %d, want COMPLETED 12", status, progress)
	}
	var completedAt string
	s.r.QueryRow(`SELECT coalesce(completed_at,'') FROM list_entry WHERE anime_id = 7`).Scan(&completedAt)

	if err := s.SetListStatus(ctx, 7, StatusRepeating, -1); err != nil {
		t.Fatal(err)
	}
	status, progress, repeat, dirty := rewatchEntry(t, s, 7)
	if status != StatusRepeating || progress != 0 || repeat != 0 || dirty != 1 {
		t.Errorf("after starting a rewatch: %s progress=%d repeat=%d dirty=%d, want REPEATING 0 0 dirty", status, progress, repeat, dirty)
	}
	var kept string
	s.r.QueryRow(`SELECT coalesce(completed_at,'') FROM list_entry WHERE anime_id = 7`).Scan(&kept)
	if kept != completedAt || kept == "" {
		t.Errorf("the first finish date was lost: %q -> %q", completedAt, kept)
	}
	var watched int
	var position, played float64
	s.r.QueryRow(`SELECT coalesce(sum(watched),0), coalesce(max(position_s),0), coalesce(max(played_s),0) FROM playback WHERE anime_id = 7`).
		Scan(&watched, &position, &played)
	if watched != 0 || position != 0 {
		t.Errorf("the first watch's ticks/positions survived: watched=%d position=%v", watched, position)
	}
	if played != 0 {
		t.Errorf("the first watch's played time survived: %v", played)
	}
	if _, ok, _ := s.LastInProgress(ctx, 7); ok {
		t.Error("a rewatch must not resume a first-watch position")
	}

	if _, err := s.MarkWatched(ctx, 7, 4); err != nil {
		t.Fatal(err)
	}
	if err := s.SetListStatus(ctx, 7, StatusRepeating, -1); err != nil {
		t.Fatal(err)
	}
	if status, progress, _, _ := rewatchEntry(t, s, 7); status != StatusRepeating || progress != 4 {
		t.Errorf("re-choosing rewatching reset it: %s %d", status, progress)
	}
}

func TestFinishingARewatchCountsIt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedCatalogue(t, s, 7, "FINISHED", 3)

	for n := 1; n <= 3; n++ {
		if _, err := s.MarkWatched(ctx, 7, n); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, repeat, _ := rewatchEntry(t, s, 7); repeat != 0 {
		t.Fatalf("a first watch counted as a rewatch: %d", repeat)
	}

	for round := 1; round <= 2; round++ {
		if err := s.SetListStatus(ctx, 7, StatusRepeating, -1); err != nil {
			t.Fatal(err)
		}
		for n := 1; n <= 3; n++ {
			if _, err := s.MarkWatched(ctx, 7, n); err != nil {
				t.Fatal(err)
			}
		}
		status, progress, repeat, _ := rewatchEntry(t, s, 7)
		if status != "COMPLETED" || progress != 3 || repeat != round {
			t.Errorf("after rewatch %d: %s %d repeat=%d", round, status, progress, repeat)
		}
	}

	entry, err := s.ListEntry(ctx, 7)
	if err != nil || entry.Repeat != 2 {
		t.Errorf("ListEntry repeat = %d (%v), want 2", entry.Repeat, err)
	}
	dirty, err := s.DirtyEntries(ctx, 0)
	if err != nil || len(dirty) != 1 || dirty[0].Repeat != 2 {
		t.Errorf("DirtyEntries = %+v (%v), want one with repeat 2", dirty, err)
	}
	if _, err := s.w.Exec(`UPDATE anime SET mal_id = 99 WHERE id = 7`); err != nil {
		t.Fatal(err)
	}
	pending, err := s.PendingMALPush(ctx, "mal", 7)
	if err != nil || pending.Repeat != 2 {
		t.Errorf("PendingMALPush repeat = %d (%v), want 2", pending.Repeat, err)
	}
}

func TestImportKeepsTheRewatchCount(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	anime := []Anime{{ID: 7, Romaji: "Show", Synonyms: "[]", Genres: "[]"}}
	rows := []Entry{{ID: 500, AnimeID: 7, Status: current(), Progress: 12, Repeat: 3}}
	if _, err := s.ImportList(ctx, anime, rows, ImportReplace); err != nil {
		t.Fatal(err)
	}
	if entry, err := s.ListEntry(ctx, 7); err != nil || entry.Repeat != 3 {
		t.Errorf("repeat = %d (%v), want 3", entry.Repeat, err)
	}
}

func TestUndoingARewatchFinishKeepsTheRewatch(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedCatalogue(t, s, 7, "FINISHED", 12)
	for n := 1; n <= 12; n++ {
		s.MarkWatched(ctx, 7, n)
	}
	if err := s.SetListStatus(ctx, 7, StatusRepeating, -1); err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 12; n++ {
		s.MarkWatched(ctx, 7, n)
	}
	if status, _, repeat, _ := rewatchEntry(t, s, 7); status != "COMPLETED" || repeat != 1 {
		t.Fatalf("rewatch finished: %s repeat=%d", status, repeat)
	}
	if err := s.Unwatch(ctx, 7, 12); err != nil {
		t.Fatal(err)
	}
	if status, progress, repeat, _ := rewatchEntry(t, s, 7); status != StatusRepeating || progress != 11 || repeat != 0 {
		t.Errorf("after the undo: %s %d repeat=%d, want REPEATING 11 repeat=0", status, progress, repeat)
	}
}

func TestPullingACompletedShowKeepsItsFinishDate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedCatalogue(t, s, 7, "FINISHED", 12)
	if _, err := s.w.Exec(`INSERT INTO list_entry (id, anime_id, status, progress, completed_at, remote_updated_at, local_updated_at)
		VALUES (70, 7, 'COMPLETED', 12, '2020-05-01', 0, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplyRemote(ctx, RemoteEntry{AnimeID: 7, Status: "COMPLETED", Progress: 12, Score: 80, Since: 10}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetListStatus(ctx, 7, "COMPLETED", -1); err != nil {
		t.Fatal(err)
	}
	var at string
	s.r.QueryRow(`SELECT completed_at FROM list_entry WHERE anime_id = 7`).Scan(&at)
	if at != "2020-05-01" {
		t.Errorf("finish date became %q", at)
	}
}
