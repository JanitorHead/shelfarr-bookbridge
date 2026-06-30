package store

import "context"

// AcquireRun atomically takes the single-flight run lock. ok=false means a run
// is already in progress.
func (s *Store) AcquireRun(ctx context.Context) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE run_state SET running=1, started_at=datetime('now'), stop_requested=0 WHERE id=1 AND running=0`)
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
	_, err := s.db.ExecContext(ctx, `UPDATE run_state SET running=0, started_at=NULL, stop_requested=0 WHERE id=1`)
	return err
}

// RequestStop asks the in-flight run to stop at the next safe point.
func (s *Store) RequestStop(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE run_state SET stop_requested=1 WHERE id=1 AND running=1`)
	return err
}

// StopRequested reports whether a stop has been requested for the current run.
func (s *Store) StopRequested(ctx context.Context) bool {
	var v int
	_ = s.db.QueryRowContext(ctx, `SELECT COALESCE(stop_requested,0) FROM run_state WHERE id=1`).Scan(&v)
	return v == 1
}
