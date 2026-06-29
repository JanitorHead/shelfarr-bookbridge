package store

import (
	"context"

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
		// always (re)assert shelf membership, even for known books
		for _, sh := range b.Shelves {
			if _, err := tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO book_shelves(source,external_id,shelf) VALUES(?,?,?)`,
				b.Source, b.ExternalID, sh); err != nil {
				return nil, err
			}
		}
		var exists int
		if err := tx.QueryRowContext(ctx,
			`SELECT 1 FROM books WHERE source=? AND external_id=?`, b.Source, b.ExternalID).Scan(&exists); err == nil {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO books(source,external_id,title,author,isbn10,year,cover_url,added_at,state)
			 VALUES(?,?,?,?,?,?,?,?, 'new')`,
			b.Source, b.ExternalID, b.Title, b.Author, b.ISBN10, b.Year, b.CoverURL,
			b.AddedAt.Format("2006-01-02T15:04:05Z07:00")); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// BaselineShelf marks current 'new' books that belong to `shelf` as 'baseline'.
func (s *Store) BaselineShelf(ctx context.Context, shelf string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE books SET state='baseline', updated_at=datetime('now')
		 WHERE state='new' AND EXISTS (
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
