package store

import "context"

// AcquireRun atomically takes the single-flight run lock. ok=false means a run
// is already in progress.
func (s *Store) AcquireRun(ctx context.Context) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE run_state SET running=1, started_at=datetime('now') WHERE id=1 AND running=0`)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// ReleaseRun releases the run lock.
func (s *Store) ReleaseRun(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE run_state SET running=0, started_at=NULL WHERE id=1`)
	return err
}
