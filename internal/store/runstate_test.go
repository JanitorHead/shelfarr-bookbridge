package store

import (
	"context"
	"testing"
)

func TestRunStateReflectsLock(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	running, _, err := s.RunState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if running {
		t.Fatal("fresh store should not report a run in progress")
	}

	if ok, err := s.AcquireRun(ctx); err != nil || !ok {
		t.Fatalf("acquire should succeed: ok=%v err=%v", ok, err)
	}
	running, startedAt, err := s.RunState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !running {
		t.Fatal("RunState should report running after AcquireRun")
	}
	if startedAt.IsZero() {
		t.Fatal("startedAt should be non-zero while a run is in progress")
	}

	if err := s.ReleaseRun(ctx); err != nil {
		t.Fatal(err)
	}
	if running, _, _ := s.RunState(ctx); running {
		t.Fatal("RunState should report idle after ReleaseRun")
	}
}
