package scheduler

import "testing"

func TestNewRejectsBadCron(t *testing.T) {
	if _, err := New("not a cron", func() {}); err == nil {
		t.Fatal("expected error for invalid cron expression")
	}
}

func TestNewAcceptsValidCronAndStartsStops(t *testing.T) {
	s, err := New("0 * * * *", func() {})
	if err != nil {
		t.Fatalf("valid cron rejected: %v", err)
	}
	s.Start()
	s.Stop() // must not panic
}
