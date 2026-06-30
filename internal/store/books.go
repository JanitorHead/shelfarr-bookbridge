package store

import (
	"context"
	"strings"
	"time"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

// fmtTime renders a time as RFC3339, or "" for the zero value.
func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02T15:04:05Z07:00")
}

// Diff records any unseen books (state='new') and returns only those that were
// not already known. Known books are left untouched. Identity = (source, external_id).
func (s *Store) Diff(ctx context.Context, books []sources.Book) ([]sources.Book, error) {
	var out []sources.Book
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, b := range books {
		// always (re)assert shelf membership, even for known books
		for _, sh := range b.Shelves {
			if _, err := tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO book_shelves(source,external_id,shelf) VALUES(?,?,?)`,
				b.Source, b.ExternalID, sh); err != nil {
				return nil, err
			}
		}
		addedAt, readAt := fmtTime(b.AddedAt), fmtTime(b.ReadAt)
		var exists int
		if err := tx.QueryRowContext(ctx,
			`SELECT 1 FROM books WHERE source=? AND external_id=?`, b.Source, b.ExternalID).Scan(&exists); err == nil {
			// Known book: refresh rich metadata (cover, rating, dates, …) without
			// touching its lifecycle state or updated_at, but only for fields the
			// source actually provided (don't clobber with blanks).
			if _, err := tx.ExecContext(ctx, `UPDATE books SET
			  cover_url      = CASE WHEN ?<>'' THEN ? ELSE cover_url END,
			  description    = CASE WHEN ?<>'' THEN ? ELSE description END,
			  year           = CASE WHEN ?<>0  THEN ? ELSE year END,
			  user_rating    = CASE WHEN ?<>0  THEN ? ELSE user_rating END,
			  average_rating = CASE WHEN ?<>0  THEN ? ELSE average_rating END,
			  added_at       = CASE WHEN ?<>'' THEN ? ELSE added_at END,
			  read_at        = CASE WHEN ?<>'' THEN ? ELSE read_at END,
			  started_at     = CASE WHEN ?<>'' THEN ? ELSE started_at END,
			  progress_pct   = CASE WHEN ?<>0  THEN ? ELSE progress_pct END,
			  progress_label = CASE WHEN ?<>'' THEN ? ELSE progress_label END
			  WHERE source=? AND external_id=?`,
				b.CoverURL, b.CoverURL, b.Description, b.Description, b.Year, b.Year,
				b.UserRating, b.UserRating, b.AverageRating, b.AverageRating,
				addedAt, addedAt, readAt, readAt,
				fmtTime(b.StartedAt), fmtTime(b.StartedAt), b.ProgressPct, b.ProgressPct, b.ProgressLabel, b.ProgressLabel,
				b.Source, b.ExternalID); err != nil {
				return nil, err
			}
			continue
		}
		// New books enter the CATALOG, not the download queue: only books in a
		// download-trigger shelf get promoted to 'new' (see PromoteDownloadable),
		// so ingesting the whole library never floods Shelfarr with read books.
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO books(source,external_id,title,author,isbn10,year,cover_url,added_at,state,description,user_rating,average_rating,read_at,started_at,progress_pct,progress_label)
			 VALUES(?,?,?,?,?,?,?,?, 'catalog', ?,?,?,?,?,?,?)`,
			b.Source, b.ExternalID, b.Title, b.Author, b.ISBN10, b.Year, b.CoverURL, addedAt,
			b.Description, b.UserRating, b.AverageRating, readAt,
			fmtTime(b.StartedAt), b.ProgressPct, b.ProgressLabel); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// BaselineShelf marks current un-actioned books (catalog/new) that belong to
// `shelf` as 'baseline' — i.e. "seen, don't request".
func (s *Store) BaselineShelf(ctx context.Context, shelf string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE books SET state='baseline', updated_at=datetime('now')
		 WHERE state IN ('new','catalog') AND EXISTS (
		   SELECT 1 FROM book_shelves bs
		   WHERE bs.source=books.source AND bs.external_id=books.external_id AND bs.shelf=?)`, shelf)
	return err
}

// ShelvesOf returns the shelves a book belongs to.
func (s *Store) ShelvesOf(ctx context.Context, source, externalID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT shelf FROM book_shelves WHERE source=? AND external_id=?`, source, externalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var sh string
		if err := rows.Scan(&sh); err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

// SetState transitions a book's lifecycle state.
func (s *Store) SetState(ctx context.Context, b sources.Book, state string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE books SET state=?, updated_at=datetime('now') WHERE source=? AND external_id=?`,
		state, b.Source, b.ExternalID)
	return err
}

// SetRequested records a successful (or already-existing) request.
func (s *Store) SetRequested(ctx context.Context, b sources.Book, workID, requestID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE books SET state='requested', work_id=?, shelfarr_request_id=?, updated_at=datetime('now')
		 WHERE source=? AND external_id=?`, workID, requestID, b.Source, b.ExternalID)
	return err
}

// SetChosenLanguage records the inferred language used for a book's request.
func (s *Store) SetChosenLanguage(ctx context.Context, b sources.Book, lang string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE books SET chosen_language=?, updated_at=datetime('now') WHERE source=? AND external_id=?`,
		lang, b.Source, b.ExternalID)
	return err
}

type ReqRef struct {
	Source     string
	ExternalID string
	RequestID  string
}

// OpenRequestItems returns books that have an in-flight Shelfarr request.
func (s *Store) OpenRequestItems(ctx context.Context) ([]ReqRef, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT source, external_id, shelfarr_request_id FROM books
		 WHERE shelfarr_request_id IS NOT NULL AND shelfarr_request_id <> ''
		   AND state IN ('requested','searching','downloading','processing')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReqRef
	for rows.Next() {
		var r ReqRef
		if err := rows.Scan(&r.Source, &r.ExternalID, &r.RequestID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ApplyStatus sets a book's state (used by reconciliation).
func (s *Store) ApplyStatus(ctx context.Context, source, externalID, state string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE books SET state=?, updated_at=datetime('now') WHERE source=? AND external_id=?`,
		state, source, externalID)
	return err
}

// NotFoundItems returns books still in not_found below the attempt cap.
func (s *Store) NotFoundItems(ctx context.Context, maxAttempts int) ([]sources.Book, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT source, external_id, title, author, COALESCE(isbn10,'') FROM books
		 WHERE state='not_found' AND attempt_count < ?`, maxAttempts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []sources.Book
	for rows.Next() {
		var b sources.Book
		if err := rows.Scan(&b.Source, &b.ExternalID, &b.Title, &b.Author, &b.ISBN10); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// IncAttempt increments and returns a book's attempt counter.
func (s *Store) IncAttempt(ctx context.Context, source, externalID string) (int, error) {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE books SET attempt_count = attempt_count + 1, updated_at=datetime('now')
		 WHERE source=? AND external_id=?`, source, externalID); err != nil {
		return 0, err
	}
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT attempt_count FROM books WHERE source=? AND external_id=?`, source, externalID).Scan(&n)
	return n, err
}

type BookRow struct {
	Source, ExternalID, Title, Author, State, WorkID, RequestID, Language, CoverURL string
	ReadingStatus                                                                   string
	AttemptCount                                                                    int
	UserRating                                                                      int
	AverageRating                                                                   float64
	AddedAt                                                                         string
	Shelves                                                                         []string
}

// ListBooks returns book rows, newest-updated first. state "" = any state; q ""
// = no text filter (otherwise matches title or author, case-insensitively).
func (s *Store) ListBooks(ctx context.Context, state, q string, limit int) ([]BookRow, error) {
	if limit <= 0 {
		limit = 500
	}
	query := `SELECT b.source,b.external_id,b.title,b.author,b.state,COALESCE(b.work_id,''),
	  COALESCE(b.shelfarr_request_id,''),COALESCE(b.chosen_language,''),b.attempt_count,COALESCE(b.cover_url,''),
	  COALESCE(b.user_rating,0),COALESCE(b.average_rating,0),COALESCE(b.added_at,''),COALESCE(b.reading_status,''),
	  COALESCE((SELECT GROUP_CONCAT(shelf, ',') FROM book_shelves bs WHERE bs.source=b.source AND bs.external_id=b.external_id),'')
	  FROM books b`
	var where []string
	args := []any{}
	if state != "" {
		where = append(where, "b.state=?")
		args = append(args, state)
	}
	if q != "" {
		where = append(where, "(b.title LIKE ? OR b.author LIKE ?)")
		like := "%" + q + "%"
		args = append(args, like, like)
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += ` ORDER BY b.updated_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BookRow
	for rows.Next() {
		var b BookRow
		var shelvesCSV string
		if err := rows.Scan(&b.Source, &b.ExternalID, &b.Title, &b.Author, &b.State, &b.WorkID, &b.RequestID, &b.Language, &b.AttemptCount, &b.CoverURL, &b.UserRating, &b.AverageRating, &b.AddedAt, &b.ReadingStatus, &shelvesCSV); err != nil {
			return nil, err
		}
		if shelvesCSV != "" {
			b.Shelves = strings.Split(shelvesCSV, ",")
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) IgnoreBook(ctx context.Context, source, externalID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE books SET state='ignored', updated_at=datetime('now') WHERE source=? AND external_id=?`, source, externalID)
	return err
}

func (s *Store) RetryBook(ctx context.Context, source, externalID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE books SET state='new', attempt_count=0, shelfarr_request_id='', updated_at=datetime('now') WHERE source=? AND external_id=?`, source, externalID)
	return err
}

// PendingNewItems returns books awaiting their first request (state='new'),
// oldest first, capped at limit. This is what drains a backlog across runs:
// Diff only records newly-discovered books, but requesting reads from here so
// books beyond a single run's quota are picked up on subsequent runs.
func (s *Store) PendingNewItems(ctx context.Context, limit int) ([]sources.Book, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT source, external_id, title, author, COALESCE(isbn10,'') FROM books
		 WHERE state='new' ORDER BY first_seen_at, rowid LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []sources.Book
	for rows.Next() {
		var b sources.Book
		if err := rows.Scan(&b.Source, &b.ExternalID, &b.Title, &b.Author, &b.ISBN10); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
