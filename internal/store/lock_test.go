package store

import (
	"context"
	"testing"
)

func TestRunLockIsSingleFlight(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ok, err := s.AcquireRun(ctx)
	if err != nil || !ok {
		t.Fatalf("first acquire should succeed: ok=%v err=%v", ok, err)
	}
	ok2, err := s.AcquireRun(ctx)
	if err != nil || ok2 {
		t.Fatalf("second acquire should fail while held: ok=%v err=%v", ok2, err)
	}
	if err := s.ReleaseRun(ctx); err != nil {
		t.Fatal(err)
	}
	ok3, _ := s.AcquireRun(ctx)
	if !ok3 {
		t.Fatal("acquire after release should succeed")
	}
}
