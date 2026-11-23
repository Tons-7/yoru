package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"yoru/internal/db"
)

type Store struct {
	r *sql.DB
	w *sql.DB

	answers atomic.Int64

	settingsMu  sync.RWMutex
	settingsGen uint64
	settings    map[string]string

	onNotify func(Notification)
}

func (s *Store) OnNotification(f func(Notification)) { s.onNotify = f }

func New(d *db.DB) *Store { return &Store{r: d.R, w: d.W} }

func (s *Store) allSettings(ctx context.Context) (map[string]string, error) {
	s.settingsMu.RLock()
	all, gen := s.settings, s.settingsGen
	s.settingsMu.RUnlock()
	if all != nil {
		return all, nil
	}
	rows, err := s.r.QueryContext(ctx, `SELECT key, value FROM setting`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	all = map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		all[k] = v
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	s.settingsMu.Lock()
	if s.settingsGen == gen {
		s.settings = all
	}
	s.settingsMu.Unlock()
	return all, nil
}

func (s *Store) settingsChanged() {
	s.settingsMu.Lock()
	s.settingsGen++
	s.settings = nil
	s.settingsMu.Unlock()
}

func (s *Store) Setting(ctx context.Context, key string) (string, error) {
	all, err := s.allSettings(ctx)
	if err != nil {
		return "", err
	}
	return all[key], nil
}

func (s *Store) SettingInt(ctx context.Context, key string) (int, error) {
	v, err := s.Setting(ctx, key)
	if err != nil || v == "" {
		return 0, err
	}
	return strconv.Atoi(v)
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	return s.SetSettings(ctx, map[string]string{key: value})
}

func (s *Store) SetSettings(ctx context.Context, kv map[string]string) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for k, v := range kv {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO setting (key, value) VALUES (?, ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v)
		if err != nil {
			return err
		}
	}
	err = tx.Commit()
	s.settingsChanged()
	return err
}

func (s *Store) EnsureSetting(ctx context.Context, key, value string) (string, error) {
	_, err := s.w.ExecContext(ctx,
		`INSERT INTO setting (key, value) VALUES (?, ?) ON CONFLICT(key) DO NOTHING`,
		key, value)
	s.settingsChanged()
	if err != nil {
		return "", err
	}
	return s.Setting(ctx, key)
}

func Secret(key string) bool {
	switch {
	case strings.HasSuffix(key, ".token"),
		strings.HasSuffix(key, "secret"),
		strings.HasSuffix(key, ".state"),
		key == "push.vapid_key", key == "skips.submitter":
		return true
	}
	return false
}

func (s *Store) Settings(ctx context.Context) (map[string]string, error) {
	all, err := s.allSettings(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(all))
	for k, v := range all {
		if !Secret(k) {
			out[k] = v
		}
	}
	return out, nil
}

func (s *Store) CountSettings(ctx context.Context) (int, error) {
	var n int
	err := s.r.QueryRowContext(ctx, `SELECT count(*) FROM setting`).Scan(&n)
	return n, err
}
