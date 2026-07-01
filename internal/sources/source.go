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
	// Rich metadata (best-effort; empty/zero when the source doesn't provide it).
	Description   string
	UserRating    int     // the user's own rating, 0–5 (0 = unrated)
	AverageRating float64 // community average
	ReadAt        time.Time
	StartedAt     time.Time
	ProgressPct   int    // 0–100 reading progress, when known (Hardcover / GR HTML)
	ProgressLabel string // e.g. "page 45 of 300"
	Review        string // the user's own review text
	Notes         string // the user's private notes
}

// Source fetches books from the enabled shelves.
type Source interface {
	Fetch(ctx context.Context, shelves []string) ([]Book, error)
}

// Shelf is a user-defined collection discovered from a source (a Goodreads
// shelf or a Hardcover list) that the user can toggle on/off for syncing.
type Shelf struct {
	Slug  string // stable identifier used by Fetch (e.g. "to-read")
	Name  string // human label
	Count int    // book count if known, else 0
}

// ShelfLister is implemented by sources that can enumerate the user's shelves
// so the GUI can present them as toggles instead of typed slugs.
type ShelfLister interface {
	ListShelves(ctx context.Context) ([]Shelf, error)
}
