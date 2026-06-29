package store

import (
	"context"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

func TestOpenRequestItemsAndApplyStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	b := sources.Book{Source: "goodreads", ExternalID: "1", Title: "A", Author: "X"}
	if _, err := s.Diff(ctx, []sources.Book{b}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRequested(ctx, b, "ol:1", "req_1"); err != nil {
		t.Fatal(err)
	}
	open, err := s.OpenRequestItems(ctx)
	if err != nil || len(open) != 1 || open[0].RequestID != "req_1" {
		t.Fatalf("open=%+v err=%v", open, err)
	}
	if err := s.ApplyStatus(ctx, "goodreads", "1", "done"); err != nil {
		t.Fatal(err)
	}
	open2, _ := s.OpenRequestItems(ctx)
	if len(open2) != 0 {
		t.Fatalf("done item should not be open, got %+v", open2)
	}
}

func TestNotFoundItemsAndIncAttempt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	b := sources.Book{Source: "goodreads", ExternalID: "9", Title: "Z", Author: "Q"}
	if _, err := s.Diff(ctx, []sources.Book{b}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetState(ctx, b, "not_found"); err != nil {
		t.Fatal(err)
	}
	items, err := s.NotFoundItems(ctx, 5)
	if err != nil || len(items) != 1 || items[0].Title != "Z" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	n, err := s.IncAttempt(ctx, "goodreads", "9")
	if err != nil || n != 1 {
		t.Fatalf("incAttempt=%d err=%v", n, err)
	}
}
