package store

import (
	"context"
	"strings"
)

// LibraryFilter selects which catalog books the Library page shows. Every field
// is optional; the zero value lists the whole library, newest-updated first.
type LibraryFilter struct {
	Status string // reading_status: to_read | reading | read | dnf
	Tag    string // a topic-shelf slug the book must belong to
	Owned  string // "owned" | "missing" — ownership in the Calibre (CWA) library
	Q      string // case-insensitive title/author substring
	Limit  int
}

// ListLibrary returns catalog books matching the filter. Unlike ListBooks (the
// download-queue view), it spans every lifecycle state by default so the Library
// shows the user's whole reading collection, not just what is being downloaded.
func (s *Store) ListLibrary(ctx context.Context, f LibraryFilter) ([]BookRow, error) {
	if f.Limit <= 0 {
		f.Limit = 1000
	}
	query := `SELECT ` + bookCols + ` FROM books b`
	var where []string
	args := []any{}
	if f.Status != "" {
		where = append(where, "b.reading_status=?")
		args = append(args, f.Status)
	}
	if f.Tag != "" {
		where = append(where, `EXISTS (SELECT 1 FROM book_shelves bs2
		  WHERE bs2.source=b.source AND bs2.external_id=b.external_id AND bs2.shelf=?)`)
		args = append(args, f.Tag)
	}
	switch f.Owned {
	case "owned":
		where = append(where, "b.owned_in_cwa=1")
	case "missing":
		where = append(where, "b.owned_in_cwa=0")
	}
	if f.Q != "" {
		where = append(where, "(b.title LIKE ? OR b.author LIKE ?)")
		like := "%" + f.Q + "%"
		args = append(args, like, like)
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += ` ORDER BY b.updated_at DESC LIMIT ?`
	args = append(args, f.Limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return scanBookRows(rows)
}

// ReadingStatusCounts returns the number of books in each reading_status
// (keys: to_read, reading, read, dnf; the empty status is omitted).
func (s *Store) ReadingStatusCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT reading_status, COUNT(*) FROM books WHERE reading_status<>'' GROUP BY reading_status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}

// TopicTagCounts returns each topic (non-status) shelf and how many books carry
// it, so the Library can offer tag filters. Status shelves (to-read, read, …)
// are excluded since they drive the reading-status filter instead.
func (s *Store) TopicTagCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT shelf, COUNT(*) FROM book_shelves GROUP BY shelf`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var shelf string
		var n int
		if err := rows.Scan(&shelf, &n); err != nil {
			return nil, err
		}
		if !IsStatusShelf(shelf) {
			out[shelf] = n
		}
	}
	return out, rows.Err()
}

// ClearOwnership resets every book's CWA-ownership flag, ahead of a fresh refresh.
func (s *Store) ClearOwnership(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE books SET owned_in_cwa=0, calibre_id=0`)
	return err
}

// SetOwnership marks a book as present in the Calibre (CWA) library, recording
// the matched Calibre book id.
func (s *Store) SetOwnership(ctx context.Context, source, externalID string, calibreID int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE books SET owned_in_cwa=1, calibre_id=? WHERE source=? AND external_id=?`,
		calibreID, source, externalID)
	return err
}

// OwnedCount returns how many books are currently flagged as owned in CWA.
func (s *Store) OwnedCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM books WHERE owned_in_cwa=1`).Scan(&n)
	return n, err
}
