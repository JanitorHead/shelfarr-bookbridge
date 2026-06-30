package store

import "context"

// Progress is the live state of an in-flight sync, surfaced to the GUI so the
// user can watch a run advance book-by-book.
type Progress struct {
	Running                     bool
	Total, Done                 int
	Requested, NotFound, Failed int
	Current                     string
}

// BeginProgress resets the live counters at the start of a run's request phase.
func (s *Store) BeginProgress(ctx context.Context, total int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE run_state SET total=?, done=0, current='', p_requested=0, p_not_found=0, p_failed=0 WHERE id=1`,
		total)
	return err
}

// SetProgress records the current item and running counters during a run.
func (s *Store) SetProgress(ctx context.Context, done int, current string, requested, notFound, failed int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE run_state SET done=?, current=?, p_requested=?, p_not_found=?, p_failed=? WHERE id=1`,
		done, current, requested, notFound, failed)
	return err
}

// Progress reads the live run progress (and whether a run is active).
func (s *Store) Progress(ctx context.Context) (Progress, error) {
	var p Progress
	var running int
	err := s.db.QueryRowContext(ctx,
		`SELECT running, total, done, current, p_requested, p_not_found, p_failed FROM run_state WHERE id=1`).
		Scan(&running, &p.Total, &p.Done, &p.Current, &p.Requested, &p.NotFound, &p.Failed)
	p.Running = running == 1
	return p, err
}
