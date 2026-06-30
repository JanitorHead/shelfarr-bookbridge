package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordAndQueryRuns(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "r.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	t1 := time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 6, 30, 11, 0, 0, 0, time.UTC)

	if _, err := st.RecordRun(ctx, RunRecord{
		StartedAt: t1, FinishedAt: t1.Add(time.Minute), Mode: "dry-run", OK: false,
		Fetched: 3, New: 1, Requested: 0, NotFound: 2, Errors: 1, Summary: "first", ErrorText: "boom",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RecordRun(ctx, RunRecord{
		StartedAt: t2, FinishedAt: t2.Add(2 * time.Minute), Mode: "apply", OK: true,
		Fetched: 10, New: 4, Requested: 4, NotFound: 1, Errors: 0, Summary: "second",
	}); err != nil {
		t.Fatal(err)
	}

	last, ok, err := st.LatestRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected a latest run")
	}
	if last.Mode != "apply" || !last.OK {
		t.Fatalf("latest run = %+v, want the second (apply, ok)", last)
	}
	if last.Fetched != 10 || last.New != 4 || last.Requested != 4 || last.NotFound != 1 || last.Errors != 0 {
		t.Fatalf("latest counters = %+v", last)
	}
	if last.Summary != "second" {
		t.Fatalf("latest summary = %q", last.Summary)
	}
	if !last.StartedAt.Equal(t2) {
		t.Fatalf("latest startedAt = %v, want %v", last.StartedAt, t2)
	}

	recent, err := st.RecentRuns(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 2 {
		t.Fatalf("recent len = %d, want 2", len(recent))
	}
	if recent[0].Mode != "apply" || recent[1].Mode != "dry-run" {
		t.Fatalf("recent order = [%s, %s], want newest-first", recent[0].Mode, recent[1].Mode)
	}
	if recent[1].ErrorText != "boom" {
		t.Fatalf("recent[1].ErrorText = %q, want boom", recent[1].ErrorText)
	}
}

func TestLatestRunEmpty(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "r.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, ok, err := st.LatestRun(context.Background()); err != nil || ok {
		t.Fatalf("LatestRun on empty store = (ok=%v, err=%v), want (false, nil)", ok, err)
	}
}
