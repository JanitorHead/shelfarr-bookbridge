package store

import (
	"context"
	"strings"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

func TestBackfillCovers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	s.Diff(ctx, []sources.Book{
		{Source: "goodreads", ExternalID: "1", Title: "A", Author: "X", ISBN10: "0262681501"},                         // no cover, has ISBN
		{Source: "goodreads", ExternalID: "2", Title: "B", Author: "Y", CoverURL: "https://src/b.jpg", ISBN10: "111"}, // has cover
		{Source: "goodreads", ExternalID: "3", Title: "C", Author: "Z"},                                               // no cover, no ISBN
	})
	n, err := s.BackfillCovers(ctx)
	if err != nil || n != 1 {
		t.Fatalf("want 1 filled, got %d err=%v", n, err)
	}
	rows, _ := s.ListLibrary(ctx, LibraryFilter{})
	byID := map[string]string{}
	for _, r := range rows {
		byID[r.ExternalID] = r.CoverURL
	}
	if !strings.Contains(byID["1"], "openlibrary.org/b/isbn/0262681501") {
		t.Fatalf("book 1 cover: %q", byID["1"])
	}
	if byID["2"] != "https://src/b.jpg" {
		t.Fatalf("book 2 source cover must win: %q", byID["2"])
	}
	if byID["3"] != "" {
		t.Fatalf("book 3 (no ISBN) must stay empty: %q", byID["3"])
	}
}
