package store

import (
	"context"
	"strings"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

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
		var exists int
		err := tx.QueryRowContext(ctx,
			`SELECT 1 FROM books WHERE source=? AND external_id=?`, b.Source, b.ExternalID).Scan(&exists)
		if err == nil {
			continue // already known
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO books(source,external_id,title,author,isbn10,year,cover_url,added_at,state,chosen_format)
			 VALUES(?,?,?,?,?,?,?,?, 'new', ?)`,
			b.Source, b.ExternalID, b.Title, b.Author, b.ISBN10, b.Year, b.CoverURL,
			b.AddedAt.Format("2006-01-02T15:04:05Z07:00"), strings.Join(b.Shelves, ",")); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// BaselineShelf marks current 'new' books whose shelves contain `shelf` as
// 'baseline' so the first run does not mass-request an existing backlog.
func (s *Store) BaselineShelf(ctx context.Context, shelf string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE books SET state='baseline', updated_at=datetime('now')
		 WHERE state='new' AND (','||chosen_format||',') LIKE ?`, "%,"+shelf+",%")
	// chosen_format temporarily carries the comma-joined shelves from Diff; see note.
	return err
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
