package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"
)

// Brings a download the engine holds under the cache budget.
func (s *Store) TrackTorrent(ctx context.Context, infoHash string, engineID int, name string) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().Unix()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO torrent (info_hash, engine_id, name, total_bytes, state, added_at)
		VALUES (?,?,?,0,'live',?)
		ON CONFLICT(info_hash) DO UPDATE SET engine_id = excluded.engine_id`,
		infoHash, engineID, name, now); err != nil {
		return err
	}

	// cache_entry has a foreign key onto torrent_file.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO torrent_file (info_hash, file_index, path, size_bytes, selected)
		VALUES (?,?,?,0,1)
		ON CONFLICT(info_hash, file_index) DO NOTHING`,
		infoHash, WholeTorrent, name); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO cache_entry (info_hash, file_index, bytes_on_disk, pinned, last_played_at)
		VALUES (?,?,0,0,?)
		ON CONFLICT(info_hash, file_index) DO NOTHING`,
		infoHash, WholeTorrent, now); err != nil {
		return err
	}
	return tx.Commit()
}

// File index for a download nobody has played, where no single file applies.
const WholeTorrent = -1

func (s *Store) HasTorrent(ctx context.Context, infoHash string) bool {
	var n int
	err := s.r.QueryRowContext(ctx,
		`SELECT count(*) FROM torrent WHERE lower(info_hash) = lower(?)`, infoHash).Scan(&n)
	return err == nil && n > 0
}

// TorrentClaimed reports a download someone recorded, not one the sweep only adopted (no episode tied to
// it). A failed read counts as claimed, as callers delete what is unclaimed.
func (s *Store) TorrentClaimed(ctx context.Context, infoHash string) bool {
	var n int
	err := s.r.QueryRowContext(ctx, `
		SELECT count(*) FROM torrent_file
		WHERE lower(info_hash) = lower(?) AND (file_index <> ? OR anime_id IS NOT NULL)`,
		infoHash, WholeTorrent).Scan(&n)
	return err != nil || n > 0
}

// TorrentFinishedOrKept: a download complete or kept on any row; a failed read counts as yes.
func (s *Store) TorrentFinishedOrKept(ctx context.Context, infoHash string) bool {
	var n int
	err := s.r.QueryRowContext(ctx, `
		SELECT count(*) FROM cache_entry WHERE lower(info_hash) = lower(?) AND (complete = 1 OR kept = 1)`,
		infoHash).Scan(&n)
	return err != nil || n > 0
}

// TorrentName is a download's top-level name as its torrent gives it; on disk it may be made safe or tagged.
type TorrentName struct {
	Name, InfoHash string
}

// TorrentNames lists the top-level names of every download on record.
func (s *Store) TorrentNames(ctx context.Context) ([]TorrentName, error) {
	rows, err := s.r.QueryContext(ctx, `
		SELECT name, info_hash FROM torrent WHERE name <> ''
		UNION SELECT path, info_hash FROM torrent_file WHERE path <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TorrentName
	for rows.Next() {
		var name, hash string
		if err := rows.Scan(&name, &hash); err != nil {
			return nil, err
		}
		if parts := strings.FieldsFunc(name, func(r rune) bool { return r == '/' || r == '\\' }); len(parts) > 0 {
			out = append(out, TorrentName{parts[0], hash})
		}
	}
	return out, rows.Err()
}

func (s *Store) DropTorrentCache(ctx context.Context, infoHash string) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM cache_entry WHERE info_hash = ?`, infoHash); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM torrent WHERE info_hash = ?`, infoHash); err != nil {
		return err
	}
	return tx.Commit()
}

type CacheEntry struct {
	InfoHash  string `json:"infoHash"`
	FileIndex int    `json:"fileIndex"`
	EngineID  *int   `json:"engineId,omitempty"`
	Bytes     int64  `json:"bytes"`
	Complete  bool   `json:"complete"`
	Pinned    bool   `json:"pinned"`
	// Kept is a download someone asked for: outside the budget, never evicted.
	Kept       bool   `json:"kept"`
	Protected  bool   `json:"protected"`
	AnimeID    *int   `json:"animeId,omitempty"`
	EpKey      string `json:"epKey,omitempty"`
	Title      string `json:"title,omitempty"`
	LastPlayed int64  `json:"lastPlayed"`
	// Name is the torrent's, which is what it is called on disk.
	Name string `json:"-"`
}

// Bytes is what the budget sees; kept downloads are reported beside it.
type CacheUsage struct {
	Bytes     int64        `json:"bytes"`
	Budget    int64        `json:"budget"`
	Files     int          `json:"files"`
	Pinned    int          `json:"pinned"`
	Kept      int          `json:"kept"`
	KeptBytes int64        `json:"keptBytes"`
	Entries   []CacheEntry `json:"entries,omitempty"`
	// Room left on the downloads drive, filled in by the server; nil when the drive can't say.
	FreeBytes *int64 `json:"freeBytes,omitempty"`
	Drive     string `json:"drive,omitempty"`
}

// torrent_file is joined loosely: only played downloads have one.
const cacheListQuery = `
SELECT c.info_hash, c.file_index, t.engine_id, c.bytes_on_disk, c.complete, c.pinned, c.kept,
       CASE WHEN e.status IN ('CURRENT','REPEATING') THEN 1 ELSE 0 END,
       f.anime_id, coalesce(f.ep_key, ''), coalesce(a.title_romaji, t.name),
       c.last_played_at, t.name
FROM cache_entry c
JOIN torrent t           ON t.info_hash = c.info_hash
LEFT JOIN torrent_file f ON f.info_hash = c.info_hash AND f.file_index = c.file_index
LEFT JOIN anime a        ON a.id = f.anime_id
LEFT JOIN list_entry e   ON e.anime_id = f.anime_id
ORDER BY c.pinned DESC, c.last_played_at DESC`

func (s *Store) CacheEntries(ctx context.Context) ([]CacheEntry, error) {
	rows, err := s.r.QueryContext(ctx, cacheListQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []CacheEntry{}
	for rows.Next() {
		var (
			e                                 CacheEntry
			complete, pinned, kept, protected int
		)
		if err := rows.Scan(&e.InfoHash, &e.FileIndex, &e.EngineID, &e.Bytes,
			&complete, &pinned, &kept, &protected, &e.AnimeID, &e.EpKey, &e.Title,
			&e.LastPlayed, &e.Name); err != nil {
			return nil, err
		}
		e.Complete, e.Pinned, e.Kept, e.Protected = complete == 1, pinned == 1, kept == 1, protected == 1
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) CacheUsage(ctx context.Context) (CacheUsage, error) {
	entries, err := s.CacheEntries(ctx)
	if err != nil {
		return CacheUsage{}, err
	}

	prefs, err := s.Prefs(ctx, 0)
	if err != nil {
		return CacheUsage{}, err
	}

	usage := CacheUsage{Budget: prefs.Int64("cache.budget_bytes"), Files: len(entries)}
	for _, e := range entries {
		if e.Pinned {
			usage.Pinned++
		}
		if e.Kept {
			usage.Kept++
			usage.KeptBytes += e.Bytes
			continue
		}
		usage.Bytes += e.Bytes
	}
	usage.Entries = entries
	return usage, nil
}

// KeepFile moves one file of a pack between the tiers; the pack row keeps whatever any of its files is.
func (s *Store) KeepFile(ctx context.Context, infoHash string, fileIndex int, kept bool) (bool, error) {
	res, err := s.w.ExecContext(ctx,
		`UPDATE cache_entry SET kept = ? WHERE info_hash = ? AND file_index = ?`,
		boolInt(kept), infoHash, fileIndex)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

type DownloadFile struct {
	FileIndex int    `json:"fileIndex"`
	EpKey     string `json:"epKey"`
	Kept      bool   `json:"kept"`
	Complete  bool   `json:"complete"`
}

// DownloadFiles lists a torrent's episodes, for the pack row on Downloads.
func (s *Store) DownloadFiles(ctx context.Context, infoHash string) ([]DownloadFile, error) {
	rows, err := s.r.QueryContext(ctx, `
		SELECT f.file_index, coalesce(f.ep_key, ''), coalesce(c.kept, 0), coalesce(c.complete, 0)
		FROM torrent_file f
		LEFT JOIN cache_entry c ON c.info_hash = f.info_hash AND c.file_index = f.file_index
		WHERE f.info_hash = ? AND f.selected = 1
		ORDER BY CAST(f.ep_key AS INTEGER), f.file_index`, infoHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []DownloadFile{}
	for rows.Next() {
		var f DownloadFile
		var kept, complete int
		if err := rows.Scan(&f.FileIndex, &f.EpKey, &kept, &complete); err != nil {
			return nil, err
		}
		f.Kept, f.Complete = kept == 1, complete == 1
		out = append(out, f)
	}
	return out, rows.Err()
}

// KeepDownload moves a whole torrent between the cache and kept tiers, as eviction is per torrent.
func (s *Store) KeepDownload(ctx context.Context, infoHash string, kept bool) (bool, error) {
	res, err := s.w.ExecContext(ctx,
		`UPDATE cache_entry SET kept = ? WHERE info_hash = ?`, boolInt(kept), infoHash)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Superseded lists an episode's replaced releases, whose files are all deselected.
func (s *Store) Superseded(ctx context.Context, animeID int, epKey string) ([]string, error) {
	rows, err := s.r.QueryContext(ctx, `
		SELECT DISTINCT f.info_hash FROM torrent_file f
		WHERE f.anime_id = ? AND f.ep_key = ? AND f.selected = 0
		  AND NOT EXISTS (SELECT 1 FROM torrent_file s
		                  WHERE s.info_hash = f.info_hash AND s.selected = 1 AND s.file_index <> ?)`,
		animeID, epKey, WholeTorrent)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, err
		}
		out = append(out, hash)
	}
	return out, rows.Err()
}

// SupersededReady: a replaced release may go now. Playing never; a finished one, or a kept one with data, only
// once the episode's new release is complete, so a dead swarm can't cost the copy the user had.
func (s *Store) SupersededReady(ctx context.Context, animeID int, epKey, infoHash string) (bool, error) {
	var pinned, held, replaced int
	err := s.r.QueryRowContext(ctx, `
		SELECT
		  EXISTS (SELECT 1 FROM cache_entry WHERE info_hash = ? AND pinned = 1),
		  EXISTS (SELECT 1 FROM cache_entry WHERE info_hash = ? AND (complete = 1 OR (kept = 1 AND bytes_on_disk > 0))),
		  EXISTS (SELECT 1 FROM torrent_file n
		          JOIN cache_entry c ON c.info_hash = n.info_hash AND (c.file_index = n.file_index OR c.file_index = ?)
		          WHERE n.anime_id = ? AND n.ep_key = ? AND n.selected = 1 AND n.info_hash <> ? AND c.complete = 1)`,
		infoHash, infoHash, WholeTorrent, animeID, epKey, infoHash).Scan(&pinned, &held, &replaced)
	if err != nil {
		return false, err
	}
	return pinned == 0 && (held == 0 || replaced == 1), nil
}

// EpisodeRef names one episode of a show.
type EpisodeRef struct {
	AnimeID int
	EpKey   string
}

// SupersededEpisodes lists episodes with a replaced release still held, for the sweep to finish dropping.
func (s *Store) SupersededEpisodes(ctx context.Context) ([]EpisodeRef, error) {
	rows, err := s.r.QueryContext(ctx, `
		SELECT DISTINCT f.anime_id, f.ep_key FROM torrent_file f
		WHERE f.selected = 0 AND f.anime_id IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM torrent_file s
		                  WHERE s.info_hash = f.info_hash AND s.selected = 1 AND s.file_index <> ?)`, WholeTorrent)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EpisodeRef
	for rows.Next() {
		var r EpisodeRef
		if err := rows.Scan(&r.AnimeID, &r.EpKey); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// WatchedEpisodes: finished in the player, or below the list's progress and
// never played here. One gone back to and left unfinished is not seen.
func (s *Store) WatchedEpisodes(ctx context.Context, animeID int) (map[int]bool, error) {
	out := map[int]bool{}
	played := map[int]bool{}
	rows, err := s.r.QueryContext(ctx,
		`SELECT ep_key, watched FROM playback WHERE anime_id = ?`, animeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var watched bool
		if err := rows.Scan(&key, &watched); err != nil {
			return nil, err
		}
		if n, err := strconv.Atoi(key); err == nil {
			played[n] = true
			if watched {
				out[n] = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// A finished show played again is a rewatch: its count covers every episode, not what was seen this time.
	var progress int
	var status sql.NullString
	err = s.r.QueryRowContext(ctx,
		`SELECT coalesce(progress, 0), status FROM list_entry WHERE anime_id = ?`, animeID).Scan(&progress, &status)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if status.String == "COMPLETED" {
		progress = 0
	}
	for n := 1; n <= progress; n++ {
		if !played[n] {
			out[n] = true
		}
	}
	return out, nil
}

func (s *Store) SetCacheBytes(ctx context.Context, infoHash string, fileIndex int, bytes int64, complete bool) error {
	_, err := s.w.ExecContext(ctx, `
		UPDATE cache_entry SET bytes_on_disk = ?, complete = ?
		WHERE info_hash = ? AND file_index = ?`,
		bytes, boolInt(complete), infoHash, fileIndex)
	return err
}

func (s *Store) PinCache(ctx context.Context, infoHash string, fileIndex int, pinned bool) error {
	_, err := s.w.ExecContext(ctx, `
		UPDATE cache_entry SET pinned = ?, last_played_at = ?
		WHERE info_hash = ? AND file_index = ?`,
		boolInt(pinned), time.Now().Unix(), infoHash, fileIndex)
	return err
}

// UnpinAll runs at startup, or a crash's leftover pin makes files unevictable for good.
func (s *Store) UnpinAll(ctx context.Context) error {
	_, err := s.w.ExecContext(ctx, `UPDATE cache_entry SET pinned = 0`)
	return err
}

// PinPlaying pins the file being watched and unpins its episode's other releases, which a mid-episode swap
// leaves with no session to unpin them. Other episodes' pins stay: another device may be playing them.
func (s *Store) PinPlaying(ctx context.Context, animeID int, epKey, infoHash string, fileIndex int) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE cache_entry SET pinned = 0
		WHERE pinned = 1 AND NOT (info_hash = ? AND file_index = ?) AND EXISTS (
		    SELECT 1 FROM torrent_file f
		    WHERE f.info_hash = cache_entry.info_hash AND (f.file_index = cache_entry.file_index OR cache_entry.file_index = ?)
		      AND f.anime_id = ? AND f.ep_key = ?)`,
		infoHash, fileIndex, WholeTorrent, animeID, epKey); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE cache_entry SET pinned = 1, last_played_at = ?
		WHERE info_hash = ? AND file_index = ?`,
		time.Now().Unix(), infoHash, fileIndex); err != nil {
		return err
	}
	return tx.Commit()
}

// CachedFile finds an episode's file, so playback can unpin it without tracking the hash.
func (s *Store) CachedFile(ctx context.Context, animeID int, epKey string) (string, int, error) {
	var hash string
	var index int
	err := s.r.QueryRowContext(ctx, `
		SELECT info_hash, file_index FROM torrent_file
		WHERE anime_id = ? AND ep_key = ? AND selected = 1
		LIMIT 1`, animeID, epKey).Scan(&hash, &index)
	if err != nil {
		return "", 0, nil
	}
	return hash, index, nil
}

// FileComplete answers from the sweeper's record, which outlives engine sessions.
func (s *Store) FileComplete(ctx context.Context, infoHash string, fileIndex int) bool {
	var complete int
	err := s.r.QueryRowContext(ctx,
		`SELECT complete FROM cache_entry WHERE info_hash = ? AND file_index = ?`,
		infoHash, fileIndex).Scan(&complete)
	return err == nil && complete == 1
}
