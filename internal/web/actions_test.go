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

func TestSyncActionRunsRunner(t *testing.T) {
	st, _ := store.Open(t.TempDir() + "/bb.db")
	defer st.Close()
	called := make(chan struct{}, 1)
	s := New(st, func(dryRun bool) (engine.Report, error) {
		called <- struct{}{}
		return engine.Report{New: 3, Requested: 2}, nil
	})
	form := url.Values{"mode": {"apply"}}
	req := httptest.NewRequest("POST", "/actions/sync", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "127.0.0.1:1"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("sync should redirect, got %d", rec.Code)
	}
	// the runner is invoked asynchronously
	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("runner was not invoked")
	}
}
