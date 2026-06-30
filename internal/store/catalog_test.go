package store

import (
	"context"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

// The whole library is ingested into the catalog, but ONLY books in a
// download-trigger shelf may be promoted to the download queue. Read/topic books
// must never be requested in Shelfarr.
func TestCatalogPromotionAndReadingStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	s.Diff(ctx, []sources.Book{
		{Source: "goodreads", ExternalID: "1", Title: "Quiero leer", Author: "X", Shelves: []string{"to-read", "ciencia"}},
		{Source: "goodreads", ExternalID: "2", Title: "Ya leído", Author: "Y", Shelves: []string{"read", "ciencia"}},
		{Source: "goodreads", ExternalID: "3", Title: "Solo tema", Author: "Z", Shelves: []string{"ciencia"}},
	})

	// everything starts in the catalog (NOT the download queue)
	if all, _ := s.ListBooks(ctx, "", "", 50); len(all) != 3 {
		t.Fatalf("want 3 catalog books, got %d", len(all))
	}
	if cat, _ := s.ListBooks(ctx, "catalog", "", 50); len(cat) != 3 {
		t.Fatalf("all 3 should start in 'catalog', got %d", len(cat))
	}

	if err := s.RefreshReadingStatus(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.PromoteDownloadable(ctx, []string{"to-read"}); err != nil {
		t.Fatal(err)
	}

	// only the to-read book becomes downloadable
	newRows, _ := s.ListBooks(ctx, "new", "", 50)
	if len(newRows) != 1 || newRows[0].ExternalID != "1" {
		t.Fatalf("only the to-read book should be downloadable: %+v", newRows)
	}
	// read + topic-only books stay in the catalog, never requested
	if cat, _ := s.ListBooks(ctx, "catalog", "", 50); len(cat) != 2 {
		t.Fatalf("read/topic books must stay in catalog, got %d", len(cat))
	}

	// reading status derived from the status shelves
	byID := map[string]string{}
	all, _ := s.ListBooks(ctx, "", "", 50)
	for _, b := range all {
		byID[b.ExternalID] = b.ReadingStatus
	}
	if byID["1"] != "to_read" || byID["2"] != "read" || byID["3"] != "" {
		t.Fatalf("reading_status wrong: %v", byID)
	}
}
