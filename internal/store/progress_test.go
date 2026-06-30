package store

import (
	"context"
	"testing"
)

func TestProgressLifecycle(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.BeginProgress(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProgress(ctx, 3, "Dune", 2, 1, 0); err != nil {
		t.Fatal(err)
	}
	p, err := s.Progress(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if p.Total != 10 || p.Done != 3 || p.Current != "Dune" || p.Requested != 2 || p.NotFound != 1 {
		t.Fatalf("progress mismatch: %+v", p)
	}
	// BeginProgress resets counters for the next run.
	if err := s.BeginProgress(ctx, 5); err != nil {
		t.Fatal(err)
	}
	p, _ = s.Progress(ctx)
	if p.Total != 5 || p.Done != 0 || p.Current != "" || p.Requested != 0 {
		t.Fatalf("progress not reset: %+v", p)
	}
}
