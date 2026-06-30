package store

import (
	"context"
	"testing"
)

func TestStopRequestFlow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if s.StopRequested(ctx) {
		t.Fatal("fresh store should not have a stop requested")
	}
	if ok, _ := s.AcquireRun(ctx); !ok {
		t.Fatal("AcquireRun failed")
	}
	if s.StopRequested(ctx) {
		t.Fatal("AcquireRun must clear any prior stop flag")
	}
	if err := s.RequestStop(ctx); err != nil {
		t.Fatal(err)
	}
	if !s.StopRequested(ctx) {
		t.Fatal("RequestStop did not set the flag")
	}
	if err := s.ReleaseRun(ctx); err != nil {
		t.Fatal(err)
	}
	if s.StopRequested(ctx) {
		t.Fatal("ReleaseRun must clear the stop flag")
	}
}
