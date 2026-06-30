package store

import (
	"context"
	"path/filepath"
	"testing"
)

// A run lock left set by a crash/restart must be cleared when the store reopens,
// otherwise every new sync fails with "a sync run is already in progress".
func TestStaleRunLockClearedOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bb.db")
	ctx := context.Background()

	s1, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := s1.AcquireRun(ctx); !ok {
		t.Fatal("AcquireRun failed")
	}
	// simulate a crash mid-run: the lock is still held, ReleaseRun never ran.
	s1.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if running, _, _ := s2.RunState(ctx); running {
		t.Fatal("reopening the store must clear a stale run lock")
	}
	if ok, _ := s2.AcquireRun(ctx); !ok {
		t.Fatal("a new run must be acquirable after the stale lock is cleared")
	}
}
