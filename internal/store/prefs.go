package store

import (
	"context"
	"database/sql"
	"errors"
	"maps"
	"strconv"
	"time"
)

var Defaults = map[string]string{
	"playback.player":      "browser",
	"playback.autoplay":    "false",
	"playback.autoskip_op": SkipButton,
	"playback.autoskip_ed": SkipButton,

	"playback.autoskip_recap":   "false",
	"playback.autoskip_preview": "false",

	"playback.skip_keep_bookends": "true",

	"playback.skip_chapters": `[]`,

	"skips.share":       "false",
	"playback.autonext": "false",

	"playback.still_watching": "3",

	"display.spoilers": SpoilersAfterNext,

	"playback.skip_filler": "false",

	"playback.voice":      "off",
	"playback.night":      "false",
	"playback.headphones": "false",
	"playback.mono":       "false",

	"playback.boost":        "off",
	"playback.anime4k":      "false",
	"playback.anime4k_mode": "A",
	"playback.anime4k_size": "M",

	"playback.mpv_hwdec":  "true",
	"playback.mpv_deband": "false",

	"playback.soften_flashing": "false",

	"playback.prepare_next": "true",

	"audio.prefer": "sub",

	"subtitle.languages": `["en"]`,

	"subtitle.with_dub": "signs",

	"subtitle.delay": "",

	"subtitle.look":  "",
	"subtitle.raise": "false",
	"schedule.mine":  "false",
	"recent.mine":    "false",

	"cast.burn_subtitles": "false",

	"display.titles": TitleEnglish,

	"home.hidden": `[]`,

	"home.adult_picks": "false",

	"settings.new_seen": "",

	"window.mode": "fullscreen",

	"window.on_close": "exit",

	"power.awake_watching":    "true",
	"power.awake_downloading": "true",

	"quality.ladder": `["1080p:BD:HEVC:10","1080p:BD","1080p:WEB","720p"]`,

	"quality.prefer":         "best",
	"quality.max_auto_bytes": "3221225472",
	"quality.allow_hi10p":    "false",

	"release.prefer_groups": `[]`,

	"release.avoid_groups":  `[]`,
	"release.exclude_terms": `[]`,

	"release.avoid_lq": "false",

	"autodownload.want": "",

	"seadex.dismissed": "",

	"cache.budget_bytes":  "5368709120",
	"cache.prefetch_next": "false",

	"cache.prefetch_count": "1",

	"cache.prefetch_full": "true",

	"downloads.while_watching": "pause",

	"downloads.limit_bytes": "0",

	"display.speed": "mbps",

	"downloads.upload": "while_downloading",

	"downloads.adapter": "",

	"discord.enabled":    "false",
	"discord.everywhere": "false",

	"cache.autodelete": "off",

	"cache.autodelete_downloads": "false",

	"sync.progress_at":  "0.9",
	"sync.poll_seconds": "900",

	"notify.enabled":      "true",
	"notify.releases":     "sub",
	"notify.poll_seconds": "1800",

	"autodownload.enabled": "false",

	"autodownload.wait_hours": "0",
}

var (
	Anime4KModes = []string{"A", "B", "C", "A+A", "B+B", "C+A"}
	Anime4KSizes = []string{"S", "M", "L", "VL", "UL"}
	AudioPrefs   = []string{"sub", "dub", "either"}
	AutoDeletes  = []string{"off", "now", "keep2"}
	Players      = []string{"mpv", "vlc", "browser"}
	TitleModes   = []string{TitleRomaji, TitleEnglish}
)

type Prefs struct {
	values map[string]string
}

func (p Prefs) String(key string) string {
	if v, ok := p.values[key]; ok && v != "" {
		return v
	}
	return Defaults[key]
}

func (p Prefs) Bool(key string) bool { return p.String(key) == "true" }

const (
	SkipOff    = "off"
	SkipButton = "button"
	SkipAuto   = "auto"
)

var SkipValues = []string{SkipOff, SkipButton, SkipAuto, "true", "false"}

func (p Prefs) SkipMode(key string) string {
	switch v := p.String(key); v {
	case SkipAuto, "true":
		return SkipAuto
	case SkipOff:
		return SkipOff
	default:
		return SkipButton
	}
}

func (p Prefs) Int(key string) int {
	n, _ := strconv.Atoi(p.String(key))
	return n
}

func (p Prefs) Int64(key string) int64 {
	n, _ := strconv.ParseInt(p.String(key), 10, 64)
	return n
}

func (p Prefs) Float(key string) float64 {
	f, _ := strconv.ParseFloat(p.String(key), 64)
	return f
}

func (p Prefs) All() map[string]string {
	out := make(map[string]string, len(Defaults))
	maps.Copy(out, Defaults)
	for k, v := range p.values {
		if v != "" {
			out[k] = v
		}
	}
	return out
}

func (s *Store) Prefs(ctx context.Context, animeID int) (Prefs, error) {
	values, err := s.Settings(ctx)
	if err != nil {
		return Prefs{}, err
	}

	if animeID != 0 {
		rows, err := s.r.QueryContext(ctx,
			`SELECT key, value FROM anime_pref WHERE anime_id = ?`, animeID)
		if err != nil {
			return Prefs{}, err
		}
		defer rows.Close()

		for rows.Next() {
			var k, v string
			if err := rows.Scan(&k, &v); err != nil {
				return Prefs{}, err
			}
			values[k] = v
		}
		if err := rows.Err(); err != nil {
			return Prefs{}, err
		}
	}
	return Prefs{values: values}, nil
}

func (s *Store) EnsureAnime(ctx context.Context, animeID int) error {
	return ensureAnime(ctx, s.w, animeID)
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func ensureAnime(ctx context.Context, db execer, animeID int) error {
	if animeID == 0 {
		return nil
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO anime (id, mal_id, title_romaji, format, episode_count, season_year, synced_at)
		SELECT c.anime_id, c.mal_id,
		       coalesce((SELECT text FROM title
		                 WHERE anime_id = c.anime_id
		                 ORDER BY (kind != 'primary'), id LIMIT 1), 'Unknown'),
		       c.kind, c.episodes, c.year, 0
		FROM corpus_anime c
		WHERE c.anime_id = ?
		ON CONFLICT(id) DO NOTHING`, animeID)
	if err != nil {
		return err
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO anime (id, title_romaji, synced_at) VALUES (?, 'Unknown', 0)
		ON CONFLICT(id) DO NOTHING`, animeID)
	return err
}

func (s *Store) Unhydrated(ctx context.Context, ids []int) ([]int, error) {
	var out []int
	for _, id := range ids {
		var synced int64
		err := s.r.QueryRowContext(ctx,
			`SELECT synced_at FROM anime WHERE id = ?`, id).Scan(&synced)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && synced == 0) {
			out = append(out, id)
			continue
		}
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) SetAnimePref(ctx context.Context, animeID int, key, value string) error {
	if value == "" {
		_, err := s.w.ExecContext(ctx,
			`DELETE FROM anime_pref WHERE anime_id = ? AND key = ?`, animeID, key)
		return err
	}
	if err := s.EnsureAnime(ctx, animeID); err != nil {
		return err
	}
	_, err := s.w.ExecContext(ctx,
		`INSERT INTO anime_pref (anime_id, key, value) VALUES (?,?,?)
		 ON CONFLICT(anime_id, key) DO UPDATE SET value = excluded.value`,
		animeID, key, value)
	return err
}

func (s *Store) AnimePrefs(ctx context.Context, animeID int) (map[string]string, error) {
	rows, err := s.r.QueryContext(ctx,
		`SELECT key, value FROM anime_pref WHERE anime_id = ?`, animeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

type Bookmark struct {
	Favourite bool   `json:"favourite"`
	Pinned    bool   `json:"pinned"`
	Hidden    bool   `json:"hidden"`
	Note      string `json:"note"`
}

func (s *Store) SetBookmark(ctx context.Context, animeID int, b Bookmark) error {
	if err := s.EnsureAnime(ctx, animeID); err != nil {
		return err
	}
	_, err := s.w.ExecContext(ctx, `
		INSERT INTO user_anime (anime_id, favourite, pinned, hidden, note, updated_at,
		                        favourite_synced, favourite_dirty)
		VALUES (?,?,?,?,?,?,0,?)
		ON CONFLICT(anime_id) DO UPDATE SET
		    favourite=excluded.favourite, pinned=excluded.pinned,
		    hidden=excluded.hidden, note=excluded.note, updated_at=excluded.updated_at,
		    favourite_dirty = CASE
		        WHEN excluded.favourite <> user_anime.favourite_synced THEN 1 ELSE 0 END`,
		animeID, boolInt(b.Favourite), boolInt(b.Pinned), boolInt(b.Hidden),
		nullable(b.Note), time.Now().Unix(), boolInt(b.Favourite))
	return err
}

type BookmarkPatch struct {
	Favourite *bool   `json:"favourite"`
	Pinned    *bool   `json:"pinned"`
	Hidden    *bool   `json:"hidden"`
	Note      *string `json:"note"`
}

func (s *Store) PatchBookmark(ctx context.Context, animeID int, p BookmarkPatch) error {
	if err := s.EnsureAnime(ctx, animeID); err != nil {
		return err
	}
	flag := func(b *bool) any {
		if b == nil {
			return nil
		}
		return boolInt(*b)
	}
	var note any
	if p.Note != nil {
		note = nullable(*p.Note)
	}
	_, err := s.w.ExecContext(ctx, `
		INSERT INTO user_anime (anime_id, favourite, pinned, hidden, note, updated_at,
		                        favourite_synced, favourite_dirty)
		VALUES (?1, coalesce(?2, 0), coalesce(?3, 0), coalesce(?4, 0), ?5, ?6, 0, coalesce(?2, 0))
		ON CONFLICT(anime_id) DO UPDATE SET
		    favourite = coalesce(?2, user_anime.favourite),
		    pinned = coalesce(?3, user_anime.pinned),
		    hidden = coalesce(?4, user_anime.hidden),
		    note = CASE WHEN ?7 THEN ?5 ELSE user_anime.note END,
		    updated_at = ?6,
		    favourite_dirty = CASE
		        WHEN coalesce(?2, user_anime.favourite) <> user_anime.favourite_synced THEN 1 ELSE 0 END`,
		animeID, flag(p.Favourite), flag(p.Pinned), flag(p.Hidden), note, time.Now().Unix(), p.Note != nil)
	return err
}

type FavouriteChange struct {
	AnimeID   int
	Favourite bool
}

func (s *Store) DirtyFavourites(ctx context.Context) ([]FavouriteChange, error) {
	rows, err := s.r.QueryContext(ctx,
		`SELECT anime_id, favourite FROM user_anime WHERE favourite_dirty = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []FavouriteChange
	for rows.Next() {
		var c FavouriteChange
		var fav int
		if err := rows.Scan(&c.AnimeID, &fav); err != nil {
			return nil, err
		}
		c.Favourite = fav == 1
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) MarkFavouriteSynced(ctx context.Context, animeID int, favourite bool) error {
	_, err := s.w.ExecContext(ctx, `
		UPDATE user_anime
		SET favourite_synced = ?,
		    favourite_dirty = CASE WHEN favourite = ? THEN 0 ELSE 1 END
		WHERE anime_id = ?`,
		boolInt(favourite), boolInt(favourite), animeID)
	return err
}

func (s *Store) ApplyRemoteFavourites(ctx context.Context, ids []int) (int, error) {
	remote := make(map[int]bool, len(ids))
	for _, id := range ids {
		remote[id] = true
	}

	rows, err := s.r.QueryContext(ctx,
		`SELECT anime_id, favourite FROM user_anime WHERE favourite_dirty = 0 AND anime_id > 0`)
	if err != nil {
		return 0, err
	}
	settled := map[int]bool{}
	for rows.Next() {
		var id, fav int
		if err := rows.Scan(&id, &fav); err != nil {
			rows.Close()
			return 0, err
		}
		settled[id] = fav == 1
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := time.Now().Unix()

	for id := range remote {
		if settled[id] {
			continue
		}
		if err := ensureAnime(ctx, tx, id); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO user_anime (anime_id, favourite, updated_at,
			                        favourite_synced, favourite_dirty)
			VALUES (?,1,?,1,0)
			ON CONFLICT(anime_id) DO UPDATE SET
			    favourite = 1, favourite_synced = 1, updated_at = excluded.updated_at
			WHERE user_anime.favourite_dirty = 0`,
			id, now); err != nil {
			return 0, err
		}
	}
	for id, fav := range settled {
		if !fav || remote[id] {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE user_anime SET favourite = 0, favourite_synced = 0, updated_at = ?
			 WHERE anime_id = ? AND favourite_dirty = 0`,
			now, id); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(remote), nil
}

func (s *Store) Bookmark(ctx context.Context, animeID int) (Bookmark, error) {
	var b Bookmark
	var fav, pin, hid int
	var note *string
	err := s.r.QueryRowContext(ctx,
		`SELECT favourite, pinned, hidden, note FROM user_anime WHERE anime_id = ?`, animeID).
		Scan(&fav, &pin, &hid, &note)
	if errors.Is(err, sql.ErrNoRows) {
		return Bookmark{}, nil
	}
	if err != nil {
		return Bookmark{}, err
	}
	b.Favourite, b.Pinned, b.Hidden = fav == 1, pin == 1, hid == 1
	if note != nil {
		b.Note = *note
	}
	return b, nil
}

const bookmarkQuery = `
SELECT ` + libraryColumns + favouritesFrom + latestPlayback + `
WHERE ` + favouritesWhere + `
ORDER BY u.pinned DESC, u.updated_at DESC
LIMIT ? OFFSET ?`

func (s *Store) Bookmarks(ctx context.Context, p Paging) (Page[LibraryItem], error) {
	items, err := s.libraryRows(ctx, bookmarkQuery, p.PerPage+1, p.Offset())
	if err != nil {
		return Page[LibraryItem]{}, err
	}
	total, err := s.countRows(ctx, `SELECT count(*)`+favouritesFrom+`WHERE `+favouritesWhere)
	if err != nil {
		return Page[LibraryItem]{}, err
	}
	return NewPage(items, p, total), nil
}

const recentQuery = `
SELECT ` + libraryColumns + `
FROM anime a
JOIN playback p2 ON p2.anime_id = a.id
LEFT JOIN list_entry e ON e.anime_id = a.id
` + latestPlayback + `
WHERE p2.last_played_at > 0
GROUP BY a.id
ORDER BY max(p2.last_played_at) DESC
LIMIT ? OFFSET ?`

func (s *Store) RecentlyWatched(ctx context.Context, p Paging) (Page[LibraryItem], error) {
	items, err := s.libraryRows(ctx, recentQuery, p.PerPage+1, p.Offset())
	if err != nil {
		return Page[LibraryItem]{}, err
	}
	total, err := s.countRows(ctx, `SELECT count(DISTINCT anime_id) FROM playback WHERE last_played_at > 0`)
	if err != nil {
		return Page[LibraryItem]{}, err
	}
	return NewPage(items, p, total), nil
}
