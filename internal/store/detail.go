package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// BookDetail is a single book with its full metadata, for the detail drawer.
type BookDetail struct {
	BookRow
	Description string
	Year        int
	ISBN10      string
}

// ErrNotFound is returned when a book id doesn't exist.
var ErrNotFound = errors.New("book not found")

// BookDetail returns one book by identity, or ErrNotFound.
func (s *Store) BookDetail(ctx context.Context, source, externalID string) (*BookDetail, error) {
	var d BookDetail
	var shelvesCSV string
	err := s.db.QueryRowContext(ctx, `SELECT `+bookCols+`,
	    COALESCE(b.description,''), COALESCE(b.year,0), COALESCE(b.isbn10,'')
	  FROM books b WHERE b.source=? AND b.external_id=?`, source, externalID).Scan(
		&d.Source, &d.ExternalID, &d.Title, &d.Author, &d.State, &d.WorkID, &d.RequestID, &d.Language,
		&d.AttemptCount, &d.CoverURL, &d.UserRating, &d.AverageRating, &d.AddedAt, &d.ReadingStatus,
		&d.StartedAt, &d.ReadAt, &d.ProgressPct, &d.ProgressLabel, &d.OwnedInCWA, &d.CalibreID, &shelvesCSV,
		&d.Description, &d.Year, &d.ISBN10)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if shelvesCSV != "" {
		d.Shelves = strings.Split(shelvesCSV, ",")
	}
	return &d, nil
}
