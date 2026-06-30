package store

import (
	"context"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

// TestLibraryFiltersAndOwnership proves the Library view spans all states,
// filters by reading status / topic tag / ownership, and that ownership flags
// round-trip and can be cleared.
func TestLibraryFiltersAndOwnership(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	s.Diff(ctx, []sources.Book{
		{Source: "goodreads", ExternalID: "1", Title: "Dune", Author: "Herbert", Shelves: []string{"read", "ciencia"}},
		{Source: "goodreads", ExternalID: "2", Title: "Sapiens", Author: "Harari", Shelves: []string{"to-read", "ciencia"}},
		{Source: "goodreads", ExternalID: "3", Title: "1984", Author: "Orwell", Shelves: []string{"currently-reading", "politica"}},
	})
	if err := s.RefreshReadingStatus(ctx); err != nil {
		t.Fatal(err)
	}

	// Whole library, no filter: all three books (catalog state included).
	all, _ := s.ListLibrary(ctx, LibraryFilter{})
	if len(all) != 3 {
		t.Fatalf("library should list all 3 catalog books, got %d", len(all))
	}

	// Reading-status filter.
	reading, _ := s.ListLibrary(ctx, LibraryFilter{Status: "reading"})
	if len(reading) != 1 || reading[0].ExternalID != "3" {
		t.Fatalf("status=reading should match only 1984, got %+v", reading)
	}

	// Topic-tag filter (ciencia is a topic, not a status shelf).
	ciencia, _ := s.ListLibrary(ctx, LibraryFilter{Tag: "ciencia"})
	if len(ciencia) != 2 {
		t.Fatalf("tag=ciencia should match 2 books, got %d", len(ciencia))
	}

	// Topic-tag counts must exclude status shelves.
	tags, _ := s.TopicTagCounts(ctx)
	if tags["ciencia"] != 2 || tags["politica"] != 1 {
		t.Fatalf("topic counts wrong: %v", tags)
	}
	if _, ok := tags["read"]; ok {
		t.Fatalf("status shelf 'read' must not appear as a topic tag: %v", tags)
	}

	// Ownership: mark one book owned, filter both ways, then clear.
	if err := s.SetOwnership(ctx, "goodreads", "1", 42); err != nil {
		t.Fatal(err)
	}
	owned, _ := s.ListLibrary(ctx, LibraryFilter{Owned: "owned"})
	if len(owned) != 1 || owned[0].CalibreID != 42 || !owned[0].OwnedInCWA {
		t.Fatalf("owned filter wrong: %+v", owned)
	}
	missing, _ := s.ListLibrary(ctx, LibraryFilter{Owned: "missing"})
	if len(missing) != 2 {
		t.Fatalf("missing filter should match 2 books, got %d", len(missing))
	}
	if n, _ := s.OwnedCount(ctx); n != 1 {
		t.Fatalf("OwnedCount=%d want 1", n)
	}
	if err := s.ClearOwnership(ctx); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.OwnedCount(ctx); n != 0 {
		t.Fatalf("after clear OwnedCount=%d want 0", n)
	}
}
