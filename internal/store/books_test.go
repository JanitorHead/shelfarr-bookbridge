package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestDiffReturnsOnlyUnknownAndPersists(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	books := []sources.Book{
		{Source: "goodreads", ExternalID: "1", Title: "A", Author: "X"},
		{Source: "goodreads", ExternalID: "2", Title: "B", Author: "Y"},
	}
	got, err := s.Diff(ctx, books)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("first Diff should return 2 new, got %d", len(got))
	}
	// second time: nothing new
	got2, err := s.Diff(ctx, books)
	if err != nil {
		t.Fatal(err)
	}
	if len(got2) != 0 {
		t.Fatalf("second Diff should return 0 new, got %d", len(got2))
	}
}

func TestBaselineExcludesFromFutureAction(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	b := sources.Book{Source: "goodreads", ExternalID: "1", Title: "A", Author: "X", Shelves: []string{"to-read"}}
	if _, err := s.Diff(ctx, []sources.Book{b}); err != nil {
		t.Fatal(err)
	}
	if err := s.BaselineShelf(ctx, "to-read"); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := s.db.QueryRow(`SELECT state FROM books WHERE external_id='1'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "baseline" {
		t.Fatalf("state = %q, want baseline", state)
	}
}
