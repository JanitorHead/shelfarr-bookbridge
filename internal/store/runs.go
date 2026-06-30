package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// RunRecord is one persisted sync outcome, written at the runOnce choke point so
// scheduled, CLI and GUI syncs are all observable.
type RunRecord struct {
	ID                                        int64
	StartedAt                                 time.Time
	FinishedAt                                time.Time
	Mode                                      string // "apply" | "dry-run"
	OK                                        bool
	Fetched, New, Requested, NotFound, Errors int
	Summary                                   string // full printReport-style line
	ErrorText                                 string
}

// RecordRun inserts a run row and returns its id.
func (s *Store) RecordRun(ctx context.Context, r RunRecord) (int64, error) {
	ok := 0
	if r.OK {
		ok = 1
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO runs(started_at, finished_at, mode, ok, fetched, new, requested, not_found, errors, summary, error_text)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		r.StartedAt.UTC().Format(time.RFC3339), r.FinishedAt.UTC().Format(time.RFC3339),
		r.Mode, ok, r.Fetched, r.New, r.Requested, r.NotFound, r.Errors, r.Summary, r.ErrorText)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// LatestRun returns the most recent run; ok=false when no runs exist.
func (s *Store) LatestRun(ctx context.Context) (RunRecord, bool, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, started_at, finished_at, mode, ok, fetched, new, requested, not_found, errors, summary, error_text
		 FROM runs ORDER BY started_at DESC, id DESC LIMIT 1`)
	r, err := scanRun(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RunRecord{}, false, nil
		}
		return RunRecord{}, false, err
	}
	return r, true, nil
}

// RecentRuns returns up to limit runs, newest first.
func (s *Store) RecentRuns(ctx context.Context, limit int) ([]RunRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, started_at, finished_at, mode, ok, fetched, new, requested, not_found, errors, summary, error_text
		 FROM runs ORDER BY started_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunRecord
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface{ Scan(dest ...any) error }

func scanRun(sc scanner) (RunRecord, error) {
	var (
		r        RunRecord
		started  string
		finished string
		ok       int
	)
	if err := sc.Scan(&r.ID, &started, &finished, &r.Mode, &ok,
		&r.Fetched, &r.New, &r.Requested, &r.NotFound, &r.Errors, &r.Summary, &r.ErrorText); err != nil {
		return RunRecord{}, err
	}
	r.OK = ok != 0
	if t, err := time.Parse(time.RFC3339, started); err == nil {
		r.StartedAt = t
	}
	if t, err := time.Parse(time.RFC3339, finished); err == nil {
		r.FinishedAt = t
	}
	return r, nil
}
