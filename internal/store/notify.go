package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"time"
)

type Notification struct {
	ID        int64          `json:"id"`
	Kind      string         `json:"kind"`
	AnimeID   *int           `json:"animeId,omitempty"`
	Episode   *int           `json:"episode,omitempty"`
	Title     string         `json:"title"`
	Body      string         `json:"body,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
	CreatedAt int64          `json:"createdAt"`
	Read      bool           `json:"read"`
}

const (
	NotifyRelease = "release"
	NotifyAiring  = "airing"
	NotifyError   = "error"
	NotifyUpdate  = "update"
	// An episode of a followed show AniList moved later, or left without a date.
	NotifyDelay = "delay"
	// A new season, sequel or film of a show on the list.
	NotifySeason = "season"
)

func (s *Store) AddNotification(ctx context.Context, n Notification) (int64, error) {
	payload := "{}"
	if len(n.Payload) > 0 {
		if b, err := json.Marshal(n.Payload); err == nil {
			payload = string(b)
		}
	}

	res, err := s.w.ExecContext(ctx, `
		INSERT INTO notification (kind, anime_id, episode, title, body, payload, created_at)
		VALUES (?,?,?,?,?,?,?)`,
		n.Kind, nullableInt(deref(n.AnimeID)), nullableInt(deref(n.Episode)),
		n.Title, nullable(n.Body), payload, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err == nil && s.onNotify != nil {
		n.ID = id
		go s.onNotify(n)
	}
	return id, err
}

func (s *Store) Notifications(ctx context.Context, unreadOnly bool, limit int) ([]Notification, error) {
	rows, err := s.r.QueryContext(ctx, `
		SELECT id, kind, anime_id, episode, title, body, payload, created_at, read_at
		FROM notification
		WHERE (? = 0 OR read_at IS NULL)
		ORDER BY created_at DESC
		LIMIT ?`, boolInt(unreadOnly), clampLimit(limit, 50, 500))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Notification{}
	for rows.Next() {
		var (
			n       Notification
			body    *string
			payload string
			readAt  *int64
		)
		if err := rows.Scan(&n.ID, &n.Kind, &n.AnimeID, &n.Episode, &n.Title,
			&body, &payload, &n.CreatedAt, &readAt); err != nil {
			return nil, err
		}
		if body != nil {
			n.Body = *body
		}
		n.Read = readAt != nil
		json.Unmarshal([]byte(payload), &n.Payload)
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) UnreadCount(ctx context.Context) (int, error) {
	var n int
	err := s.r.QueryRowContext(ctx,
		`SELECT count(*) FROM notification WHERE read_at IS NULL`).Scan(&n)
	return n, err
}

// MarkRead with id 0 marks everything, which is what dismissing the panel does.
func (s *Store) MarkRead(ctx context.Context, id int64) error {
	now := time.Now().Unix()
	if id == 0 {
		_, err := s.w.ExecContext(ctx,
			`UPDATE notification SET read_at = ? WHERE read_at IS NULL`, now)
		return err
	}
	_, err := s.w.ExecContext(ctx,
		`UPDATE notification SET read_at = ? WHERE id = ? AND read_at IS NULL`, now, id)
	return err
}

// DeleteNotification removes one, or all when id is zero; what was announced is recorded apart from the list.
func (s *Store) DeleteNotification(ctx context.Context, id int64) (int, error) {
	var (
		res interface{ RowsAffected() (int64, error) }
		err error
	)
	if id == 0 {
		res, err = s.w.ExecContext(ctx, `DELETE FROM notification`)
	} else {
		res, err = s.w.ExecContext(ctx, `DELETE FROM notification WHERE id = ?`, id)
	}
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// AlreadyNotified keys on the episode, not the release: many groups sub the same episode, one story.
func (s *Store) AlreadyNotified(ctx context.Context, infoHash string, animeID, episode int) (bool, error) {
	var one int
	err := s.r.QueryRowContext(ctx,
		`SELECT 1 FROM notified_release WHERE info_hash = ?`, infoHash).Scan(&one)
	if err == nil {
		return true, nil
	}
	if animeID == 0 || episode <= 0 {
		return false, nil
	}

	err = s.r.QueryRowContext(ctx,
		`SELECT 1 FROM notified_release WHERE anime_id = ? AND episode = ? AND announced = 1`,
		animeID, episode).Scan(&one)
	return err == nil, nil
}

func (s *Store) RecordNotified(ctx context.Context, infoHash string, animeID, episode int) error {
	return s.recordRelease(ctx, infoHash, animeID, episode, true)
}

// RecordFetched notes a release taken for auto-download without telling anyone: the episode stays news.
func (s *Store) RecordFetched(ctx context.Context, infoHash string, animeID, episode int) error {
	return s.recordRelease(ctx, infoHash, animeID, episode, false)
}

// EpisodeFetched: some release of the episode was already taken, announced or not.
func (s *Store) EpisodeFetched(ctx context.Context, animeID, episode int) bool {
	var one int
	return s.r.QueryRowContext(ctx,
		`SELECT 1 FROM notified_release WHERE anime_id = ? AND episode = ?`, animeID, episode).Scan(&one) == nil
}

// AiringDelay is an episode AniList moved more than a day later, or (NowAt nil) whose future date it dropped.
type AiringDelay struct {
	AnimeID, Episode int
	WasAt            int64
	NowAt            *int64
}

// UnannouncedDelays are the delays the release watch hasn't yet told of, or let go.
func (s *Store) UnannouncedDelays(ctx context.Context) ([]AiringDelay, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT anime_id, episode, was_at, now_at FROM airing_delay WHERE announced = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AiringDelay
	for rows.Next() {
		var d AiringDelay
		if err := rows.Scan(&d.AnimeID, &d.Episode, &d.WasAt, &d.NowAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) MarkDelayAnnounced(ctx context.Context, animeID, episode int) error {
	_, err := s.w.ExecContext(ctx, `UPDATE airing_delay SET announced = 1 WHERE anime_id = ? AND episode = ?`, animeID, episode)
	return err
}

// ShowDelay is the show's latest episode AniList moved, while it hasn't aired; ok false when none was moved.
func (s *Store) ShowDelay(ctx context.Context, animeID int) (d AiringDelay, ok bool) {
	// Not once it has aired, by its new date or AniList moving on past it, nor for a finished show.
	err := s.r.QueryRowContext(ctx, `
		SELECT d.anime_id, d.episode, d.was_at, d.now_at FROM airing_delay d JOIN anime a ON a.id = d.anime_id
		WHERE d.anime_id = ? AND (d.now_at IS NULL OR d.now_at > unixepoch())
		  AND coalesce(a.status, '') <> 'FINISHED' AND (a.next_episode IS NULL OR a.next_episode <= d.episode)
		ORDER BY d.episode DESC LIMIT 1`, animeID).Scan(&d.AnimeID, &d.Episode, &d.WasAt, &d.NowAt)
	return d, err == nil
}

// Notified: a notification of this kind about the show exists, read or not.
func (s *Store) Notified(ctx context.Context, kind string, animeID int) bool {
	var one int
	return s.r.QueryRowContext(ctx, `SELECT 1 FROM notification WHERE kind = ? AND anime_id = ? LIMIT 1`,
		kind, animeID).Scan(&one) == nil
}

// HeldEpisode: some release of the episode is downloaded or downloading, or a library file has it.
func (s *Store) HeldEpisode(ctx context.Context, animeID, episode int) bool {
	var one int
	return s.r.QueryRowContext(ctx, `
		SELECT 1 FROM torrent_file WHERE anime_id = ? AND ep_key = ? AND selected = 1
		UNION ALL
		SELECT 1 FROM local_file WHERE anime_id = ? AND episode = ? AND missing = 0
		LIMIT 1`, animeID, strconv.Itoa(episode), animeID, episode).Scan(&one) == nil
}

// TakenAt is when the release watch last took or announced a release of the episode.
func (s *Store) TakenAt(ctx context.Context, animeID, episode int) (time.Time, bool) {
	var at sql.NullInt64
	s.r.QueryRowContext(ctx, `SELECT max(created_at) FROM notified_release WHERE anime_id = ? AND episode = ?`,
		animeID, episode).Scan(&at)
	return time.Unix(at.Int64, 0), at.Valid
}

// ReleaseWait is a new episode auto-download holds for a better release.
type ReleaseWait struct {
	AnimeID   int       `json:"animeId"`
	Episode   int       `json:"episode"`
	FirstSeen time.Time `json:"-"`
	Title     string    `json:"title"`
	Cover     *string   `json:"cover,omitempty"`
}

// WaitSince starts the episode's wait if it has none, and says when it began.
func (s *Store) WaitSince(ctx context.Context, animeID, episode int) (time.Time, error) {
	if _, err := s.w.ExecContext(ctx, `INSERT INTO release_wait (anime_id, episode, first_seen) VALUES (?,?,?)
		ON CONFLICT DO NOTHING`, animeID, episode, time.Now().Unix()); err != nil {
		return time.Time{}, err
	}
	var at int64
	err := s.r.QueryRowContext(ctx, `SELECT first_seen FROM release_wait WHERE anime_id = ? AND episode = ?`,
		animeID, episode).Scan(&at)
	return time.Unix(at, 0), err
}

// Waits lists the episodes held for a better release, of one show or (animeID 0) all, oldest first.
func (s *Store) Waits(ctx context.Context, animeID int) ([]ReleaseWait, error) {
	mode := s.TitleMode(ctx, 0)
	rows, err := s.r.QueryContext(ctx, `
		SELECT w.anime_id, w.episode, w.first_seen, coalesce(a.title_romaji, ''), coalesce(a.title_english, ''),
		       a.cover_url
		FROM release_wait w LEFT JOIN anime a ON a.id = w.anime_id
		WHERE ? = 0 OR w.anime_id = ?
		ORDER BY w.first_seen, w.anime_id, w.episode`, animeID, animeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReleaseWait
	for rows.Next() {
		var w ReleaseWait
		var at int64
		var romaji, english string
		if err := rows.Scan(&w.AnimeID, &w.Episode, &at, &romaji, &english, &w.Cover); err != nil {
			return nil, err
		}
		w.FirstSeen, w.Title = time.Unix(at, 0), PickTitle(mode, romaji, &english)
		out = append(out, w)
	}
	return out, rows.Err()
}

// EndWait forgets an episode's wait, when it's taken or no longer wanted.
func (s *Store) EndWait(ctx context.Context, animeID, episode int) error {
	_, err := s.w.ExecContext(ctx, `DELETE FROM release_wait WHERE anime_id = ? AND episode = ?`, animeID, episode)
	return err
}

// FirstRecorded is the lowest episode of the show the release watch took or announced, 0 if none.
func (s *Store) FirstRecorded(ctx context.Context, animeID int) int {
	var ep sql.NullInt64
	s.r.QueryRowContext(ctx, `SELECT min(episode) FROM notified_release WHERE anime_id = ? AND episode > 0`, animeID).Scan(&ep)
	return int(ep.Int64)
}

func (s *Store) recordRelease(ctx context.Context, infoHash string, animeID, episode int, announced bool) error {
	_, err := s.w.ExecContext(ctx, `
		INSERT INTO notified_release (info_hash, anime_id, episode, created_at, announced)
		VALUES (?,?,?,?,?) ON CONFLICT DO UPDATE SET announced = max(announced, excluded.announced)`,
		infoHash, nullableInt(animeID), nullableInt(episode), time.Now().Unix(), boolInt(announced))
	return err
}

type Follow struct {
	AnimeID  int
	Titles   []string
	Episodes int
	Progress int

	// The episode the catalogue says airs next, and when; zero once the show has finished or before a sync.
	NextEpisode  int
	NextAiringAt int64
	EndDate      string // YYYY-MM-DD, empty while airing or unknown
}

func (f Follow) Aired(episode int, now int64) bool {
	return aired(episode, f.NextEpisode, f.NextAiringAt, now)
}

// Latest is the episode that aired last, or 0: the one before the next (the next itself once its time
// has passed), or the last of a show that ended this week.
func (f Follow) Latest(now int64) int {
	switch {
	case f.NextEpisode > 0 && f.NextAiringAt > 0 && f.NextAiringAt <= now:
		return f.NextEpisode
	case f.NextEpisode > 0:
		return f.NextEpisode - 1
	}
	if ended, err := time.Parse("2006-01-02", f.EndDate); err == nil && now-ended.Unix() < 7*24*3600 {
		return f.Episodes
	}
	return 0
}

// aired: only the next episode's time is known; anything past it airs later still, however stale the row.
func aired(episode, nextEpisode int, airingAt, now int64) bool {
	if nextEpisode <= 0 || airingAt <= 0 || episode < nextEpisode {
		return true
	}
	if episode > nextEpisode {
		return false
	}
	return airingAt <= now
}

// EpisodeAired: has the episode broadcast, and when does the next; no schedule counts as aired.
func (s *Store) EpisodeAired(ctx context.Context, animeID, episode int) (bool, int64) {
	var next, at *int
	err := s.r.QueryRowContext(ctx,
		`SELECT next_episode, next_airing_at FROM anime WHERE id = ?`, animeID).Scan(&next, &at)
	if err != nil || next == nil || at == nil {
		return true, 0
	}
	return aired(episode, *next, int64(*at), time.Now().Unix()), int64(*at)
}

// Watched: followed, auto-download, or listed as watching; grouped, so a show that is several is searched once.
const followQuery = `
	SELECT anime_id, max(episode_count), max(progress),
	       max(next_episode), max(next_airing_at), max(end_date)
	FROM (
		SELECT f.anime_id AS anime_id,
		       coalesce(a.episode_count,0) AS episode_count,
		       coalesce(e.progress,0) AS progress,
		       coalesce(a.next_episode,0) AS next_episode,
		       coalesce(a.next_airing_at,0) AS next_airing_at,
		       coalesce(a.end_date,'') AS end_date
		FROM follow f
		LEFT JOIN anime a ON a.id = f.anime_id
		LEFT JOIN list_entry e ON e.anime_id = f.anime_id
		-- A show with no known episode count has no last episode to stop at; finishing or dropping it does.
		WHERE coalesce(e.status, '') NOT IN ('COMPLETED', 'DROPPED')

		UNION ALL

		SELECT p.anime_id,
		       coalesce(a.episode_count,0), coalesce(e.progress,0),
		       coalesce(a.next_episode,0), coalesce(a.next_airing_at,0), coalesce(a.end_date,'')
		FROM anime_pref p
		LEFT JOIN anime a ON a.id = p.anime_id
		LEFT JOIN list_entry e ON e.anime_id = p.anime_id
		WHERE (p.key = 'autodownload.enabled' AND p.value = 'true' OR p.key = 'autodownload.want' AND p.value <> '')
		  AND coalesce(e.status, '') NOT IN ('COMPLETED', 'DROPPED')

		UNION ALL

		-- A watching status synced from the tracker followed nothing, so nobody heard; muted on its page stays quiet.
		SELECT e.anime_id,
		       coalesce(a.episode_count,0), coalesce(e.progress,0),
		       coalesce(a.next_episode,0), coalesce(a.next_airing_at,0), coalesce(a.end_date,'')
		FROM list_entry e
		LEFT JOIN anime a ON a.id = e.anime_id
		WHERE e.status IN ('CURRENT', 'REPEATING')
		  AND NOT EXISTS (SELECT 1 FROM anime_pref p
		                  WHERE p.anime_id = e.anime_id AND p.key = 'notify.enabled' AND p.value = 'false')
	)
	GROUP BY anime_id`

func (s *Store) Follows(ctx context.Context) ([]Follow, error) {
	rows, err := s.r.QueryContext(ctx, followQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Follow
	for rows.Next() {
		var f Follow
		if err := rows.Scan(&f.AnimeID, &f.Episodes, &f.Progress, &f.NextEpisode, &f.NextAiringAt, &f.EndDate); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range out {
		out[i].Titles, _ = s.SearchTitles(ctx, out[i].AnimeID)
	}
	return out, nil
}

func (s *Store) SetFollow(ctx context.Context, animeID int, on bool) error {
	if !on {
		return s.Unfollow(ctx, animeID)
	}
	if err := s.EnsureAnime(ctx, animeID); err != nil {
		return err
	}
	_, err := s.w.ExecContext(ctx, `
		INSERT INTO follow (anime_id, created_at) VALUES (?,?)
		ON CONFLICT(anime_id) DO NOTHING`,
		animeID, time.Now().Unix())
	return err
}

// FollowState says whether new episodes of the show alert, and why: "followed", "watching" or "auto". Wanted is
// an episode to download when it's out, 0 for none.
type FollowState struct {
	On      bool   `json:"on"`
	Because string `json:"because,omitempty"`
	Wanted  int    `json:"wanted,omitempty"`
}

// FollowOf mirrors the release watch's own rules (followQuery and the per-show mute).
func (s *Store) FollowOf(ctx context.Context, animeID int) (FollowState, error) {
	var followed int
	var status, mute, auto, want string
	err := s.r.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM follow WHERE anime_id = ?1),
		       coalesce((SELECT status FROM list_entry WHERE anime_id = ?1), ''),
		       coalesce((SELECT value FROM anime_pref WHERE anime_id = ?1 AND key = 'notify.enabled'), ''),
		       coalesce((SELECT value FROM anime_pref WHERE anime_id = ?1 AND key = 'autodownload.enabled'), ''),
		       coalesce((SELECT value FROM anime_pref WHERE anime_id = ?1 AND key = 'autodownload.want'), '')`,
		animeID).Scan(&followed, &status, &mute, &auto, &want)
	if err != nil {
		return FollowState{}, err
	}
	var out FollowState
	out.Wanted, _ = strconv.Atoi(want)
	// Finished or dropped, a follow stops counting, as in followQuery.
	listed := status != "COMPLETED" && status != "DROPPED"
	switch {
	case mute == "false":
	case followed == 1 && listed:
		out.On, out.Because = true, "followed"
	case status == "CURRENT" || status == "REPEATING":
		out.On, out.Because = true, "watching"
	case auto == "true" && listed:
		out.On, out.Because = true, "auto"
	case out.Wanted > 0:
		out.On, out.Because = true, "followed"
	}
	return out, nil
}

// SetNotify turns a show's new-episode alerts on (a follow, the mute lifted) or off (unfollowed and muted, as
// a show on Watching or with auto-download alerts on its own).
func (s *Store) SetNotify(ctx context.Context, animeID int, on bool) error {
	if err := s.SetFollow(ctx, animeID, on); err != nil {
		return err
	}
	mute := ""
	if !on {
		mute = "false"
	}
	return s.SetAnimePref(ctx, animeID, "notify.enabled", mute)
}

func (s *Store) Unfollow(ctx context.Context, animeID int) error {
	_, err := s.w.ExecContext(ctx, `DELETE FROM follow WHERE anime_id = ?`, animeID)
	return err
}

func deref(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

func nullableInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}
