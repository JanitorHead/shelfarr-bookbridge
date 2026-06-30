package store

import (
	"context"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

func TestSettingsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, ok, _ := s.GetSetting(ctx, "SCHEDULE"); ok {
		t.Fatal("unset key should report ok=false")
	}
	if err := s.SetSetting(ctx, "SCHEDULE", "*/30 * * * *"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(ctx, "SCHEDULE", "0 * * * *"); err != nil { // upsert
		t.Fatal(err)
	}
	v, ok, _ := s.GetSetting(ctx, "SCHEDULE")
	if !ok || v != "0 * * * *" {
		t.Fatalf("got %q ok=%v", v, ok)
	}
	all, _ := s.AllSettings(ctx)
	if all["SCHEDULE"] != "0 * * * *" {
		t.Fatalf("AllSettings: %v", all)
	}
}

func TestStateCounts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	s.Diff(ctx, []sources.Book{
		{Source: "goodreads", ExternalID: "1", Title: "A", Author: "X"},
		{Source: "goodreads", ExternalID: "2", Title: "B", Author: "Y"},
	})
	s.SetState(ctx, sources.Book{Source: "goodreads", ExternalID: "1"}, "requested")
	c, _ := s.StateCounts(ctx)
	if c["new"] != 1 || c["requested"] != 1 {
		t.Fatalf("counts: %v", c)
	}
}
