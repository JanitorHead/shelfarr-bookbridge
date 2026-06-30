package store

import (
	"context"
	"database/sql"
	"time"
)

// RunState reports whether a sync is currently in progress (the single-flight
// lock held by AcquireRun) and, if so, when it started.
func (s *Store) RunState(ctx context.Context) (running bool, startedAt time.Time, err error) {
	var (
		run     int
		started sql.NullString
	)
	row := s.db.QueryRowContext(ctx, `SELECT running, started_at FROM run_state WHERE id=1`)
	if err = row.Scan(&run, &started); err != nil {
		return false, time.Time{}, err
	}
	if started.Valid && started.String != "" {
		// AcquireRun writes datetime('now') (UTC, "2006-01-02 15:04:05").
		if t, perr := time.Parse("2006-01-02 15:04:05", started.String); perr == nil {
			startedAt = t.UTC()
		}
	}
	return run != 0, startedAt, nil
}
