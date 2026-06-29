package sources

import (
	"context"
	"time"
)

// Book is the normalized record produced by every Source.
// Identity is (Source, ExternalID); AddedAt is metadata only.
type Book struct {
	Source     string // e.g. "goodreads"
	ExternalID string // Goodreads book_id — always present
	Title      string
	Author     string
	ISBN10     string // may be empty
	Shelves    []string
	AddedAt    time.Time
	Year       int
	CoverURL   string
}

// Source fetches books from the enabled shelves.
type Source interface {
	Fetch(ctx context.Context, shelves []string) ([]Book, error)
}
