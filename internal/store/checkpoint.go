package store

import "context"

func (s *Store) SnapshotTo(ctx context.Context, path string) error {
	_, err := s.w.ExecContext(ctx, `VACUUM INTO ?`, path)
	return err
}
