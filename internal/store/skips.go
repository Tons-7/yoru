package store

import (
	"context"
	"encoding/binary"
	"time"

	"yoru/internal/player"
)

func (s *Store) OwnSkips(ctx context.Context, animeID, episode int) ([]player.SkipRange, error) {
	rows, err := s.r.QueryContext(ctx, `
		SELECT kind, start_s, end_s, source FROM skip_own
		WHERE anime_id = ? AND episode = ? ORDER BY start_s`, animeID, episode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []player.SkipRange
	for rows.Next() {
		var r player.SkipRange
		if err := rows.Scan(&r.Kind, &r.Start, &r.End, &r.Source); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) SaveOwnSkip(ctx context.Context, animeID, episode int, r player.SkipRange) error {
	_, err := s.w.ExecContext(ctx, `
		INSERT INTO skip_own (anime_id, episode, kind, start_s, end_s, source) VALUES (?,?,?,?,?,?)
		ON CONFLICT(anime_id, episode, kind, source) DO UPDATE SET start_s = excluded.start_s, end_s = excluded.end_s`,
		animeID, episode, r.Kind, r.Start, r.End, r.Source)
	return err
}

func (s *Store) ForgetMarks(ctx context.Context, animeID, episode int) error {
	_, err := s.w.ExecContext(ctx, `DELETE FROM skip_own WHERE anime_id = ? AND episode = ? AND source = 'marked'`,
		animeID, episode)
	return err
}

func (s *Store) SkipPrint(ctx context.Context, animeID int, kind string) ([]uint32, bool) {
	var blob []byte
	err := s.r.QueryRowContext(ctx, `SELECT print FROM skip_print WHERE anime_id = ? AND kind = ?`, animeID, kind).Scan(&blob)
	if err != nil || len(blob) < 4 {
		return nil, false
	}
	out := make([]uint32, len(blob)/4)
	for i := range out {
		out[i] = binary.LittleEndian.Uint32(blob[i*4:])
	}
	return out, true
}

func (s *Store) SaveSkipPrint(ctx context.Context, animeID int, kind string, print []uint32) error {
	blob := make([]byte, len(print)*4)
	for i, v := range print {
		binary.LittleEndian.PutUint32(blob[i*4:], v)
	}
	_, err := s.w.ExecContext(ctx, `
		INSERT INTO skip_print (anime_id, kind, print, found_at) VALUES (?,?,?,?)
		ON CONFLICT(anime_id, kind) DO UPDATE SET print = excluded.print, found_at = excluded.found_at`,
		animeID, kind, blob, time.Now().Unix())
	return err
}

func (s *Store) MarkShared(ctx context.Context, key string) (bool, error) {
	res, err := s.w.ExecContext(ctx, `INSERT INTO skip_shared (key, sent_at) VALUES (?, ?) ON CONFLICT DO NOTHING`,
		key, time.Now().Unix())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (s *Store) UnmarkShared(ctx context.Context, key string) {
	s.w.ExecContext(ctx, `DELETE FROM skip_shared WHERE key = ?`, key)
}
