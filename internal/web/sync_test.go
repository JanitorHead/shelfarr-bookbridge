package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/engine"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/store"
)

func TestSyncIsNonBlocking(t *testing.T) {
	st, _ := store.Open(t.TempDir() + "/bb.db")
	defer st.Close()
	block := make(chan struct{})
	started := make(chan struct{})
	s := New(st, func(dryRun bool) (engine.Report, error) {
		close(started)
		<-block // hold the run open to prove the handler doesn't wait on it
		return engine.Report{}, nil
	})
	defer close(block)

	form := url.Values{"mode": {"apply"}}
	req := httptest.NewRequest("POST", "/actions/sync", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "127.0.0.1:1"
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() { s.Handler().ServeHTTP(rec, req); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleSync blocked on the runner instead of returning immediately")
	}
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("sync should redirect (303), got %d", rec.Code)
	}
	// the runner goroutine should have been launched
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("runner goroutine was never started")
	}
}

func TestStatusEndpoint(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest("GET", "/actions/status", nil)
	req.RemoteAddr = "127.0.0.1:1"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status should be 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("status should be JSON, got %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "\"running\"") {
		t.Fatalf("status JSON should contain a running field: %s", rec.Body.String())
	}
}
