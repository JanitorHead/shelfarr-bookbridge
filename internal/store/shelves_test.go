package store

import (
	"context"
	"sort"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

func TestDiffPopulatesBookShelves(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	b := sources.Book{Source: "goodreads", ExternalID: "1", Title: "A", Author: "X", Shelves: []string{"to-read", "sci-fi"}}
	if _, err := s.Diff(ctx, []sources.Book{b}); err != nil {
		t.Fatal(err)
	}
	got, err := s.ShelvesOf(ctx, "goodreads", "1")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	if len(got) != 2 || got[0] != "sci-fi" || got[1] != "to-read" {
		t.Fatalf("book_shelves = %v, want [sci-fi to-read]", got)
	}
}

func TestBaselineUsesBookShelves(t *testing.T) {
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
		t.Fatalf("state=%q, want baseline", state)
	}
}

func TestSchemaVersionIsThree(t *testing.T) {
	s := newTestStore(t)
	var ver int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != schemaVersion || schemaVersion != 3 {
		t.Fatalf("user_version=%d schemaVersion=%d, want 3", ver, schemaVersion)
	}
}
