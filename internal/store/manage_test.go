package store

import (
	"context"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

func TestListBooksAndActions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	s.Diff(ctx, []sources.Book{
		{Source: "goodreads", ExternalID: "1", Title: "A", Author: "X"},
		{Source: "goodreads", ExternalID: "2", Title: "B", Author: "Y"},
	})
	s.SetState(ctx, sources.Book{Source: "goodreads", ExternalID: "2"}, "not_found")
	if rows, _ := s.ListBooks(ctx, "not_found", "", 50); len(rows) != 1 || rows[0].ExternalID != "2" {
		t.Fatalf("ListBooks filter: %+v", rows)
	}
	if all, _ := s.ListBooks(ctx, "", "", 50); len(all) != 2 {
		t.Fatalf("ListBooks all: %d", len(all))
	}
	if got, _ := s.ListBooks(ctx, "", "B", 50); len(got) != 1 || got[0].ExternalID != "2" {
		t.Fatalf("ListBooks search q=B: %+v", got)
	}
	if err := s.IgnoreBook(ctx, "goodreads", "1"); err != nil {
		t.Fatal(err)
	}
	if rows, _ := s.ListBooks(ctx, "ignored", "", 50); len(rows) != 1 {
		t.Fatal("ignore failed")
	}
	if err := s.RetryBook(ctx, "goodreads", "2"); err != nil {
		t.Fatal(err)
	}
	if rows, _ := s.ListBooks(ctx, "new", "", 50); len(rows) != 1 || rows[0].ExternalID != "2" {
		t.Fatalf("retry should reset to new: %+v", rows)
	}
}

func TestShelfConfig(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	configured := []string{"to-read", "sci-fi"}
	if err := s.SetShelfConfig(ctx, "sci-fi", false, "audiobook", ""); err != nil {
		t.Fatal(err)
	}
	cfgs, _ := s.ShelfConfigs(ctx, configured)
	if len(cfgs) != 2 {
		t.Fatalf("want 2 shelf cfgs, got %d", len(cfgs))
	}
	en, _ := s.EnabledShelves(ctx, configured)
	if len(en) != 1 || en[0] != "to-read" {
		t.Fatalf("EnabledShelves should drop disabled sci-fi: %v", en)
	}
	if f, ok := s.ShelfFormat(ctx, "sci-fi"); !ok || f != "audiobook" {
		t.Fatalf("ShelfFormat: %q %v", f, ok)
	}
}
