package store

import (
	"context"
	"time"
)

type PushSubscription struct {
	Endpoint string `json:"endpoint"`
	P256dh   string `json:"p256dh"`
	Auth     string `json:"auth"`
	Origin   string `json:"origin"`
	Device   string `json:"device"`
	Paired   bool   `json:"paired"`
}

func (s *Store) SavePushSubscription(ctx context.Context, sub PushSubscription) error {
	_, err := s.w.ExecContext(ctx, `
		INSERT INTO push_subscription (endpoint, p256dh, auth, origin, device, paired, created_at) VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(endpoint) DO UPDATE SET p256dh = excluded.p256dh, auth = excluded.auth, origin = excluded.origin,
			device = excluded.device, paired = excluded.paired`,
		sub.Endpoint, sub.P256dh, sub.Auth, sub.Origin, sub.Device, sub.Paired, time.Now().Unix())
	return err
}

func (s *Store) DeletePushSubscription(ctx context.Context, endpoint string) error {
	_, err := s.w.ExecContext(ctx, `DELETE FROM push_subscription WHERE endpoint = ?`, endpoint)
	return err
}

func (s *Store) DeleteDevicePush(ctx context.Context, device string) error {
	_, err := s.w.ExecContext(ctx, `DELETE FROM push_subscription WHERE device = ? AND device != ''`, device)
	return err
}

func (s *Store) DeleteAnonymousPairedPush(ctx context.Context) error {
	_, err := s.w.ExecContext(ctx, `DELETE FROM push_subscription WHERE paired = 1 AND device = ''`)
	return err
}

func (s *Store) DeletePairedPush(ctx context.Context) error {
	_, err := s.w.ExecContext(ctx, `DELETE FROM push_subscription WHERE paired = 1`)
	return err
}

func (s *Store) PushSubscriptions(ctx context.Context) ([]PushSubscription, error) {
	rows, err := s.r.QueryContext(ctx, `SELECT endpoint, p256dh, auth, origin, device, paired FROM push_subscription`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PushSubscription
	for rows.Next() {
		var sub PushSubscription
		if err := rows.Scan(&sub.Endpoint, &sub.P256dh, &sub.Auth, &sub.Origin, &sub.Device, &sub.Paired); err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}
