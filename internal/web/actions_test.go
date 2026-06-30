package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/engine"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/store"
)

func TestSyncActionRunsRunner(t *testing.T) {
	st, _ := store.Open(t.TempDir() + "/bb.db")
	defer st.Close()
	called := false
	s := New(st, func(dryRun bool) (engine.Report, error) { called = true; return engine.Report{New: 3, Requested: 2}, nil })
	form := url.Values{"mode": {"apply"}}
	req := httptest.NewRequest("POST", "/actions/sync", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "127.0.0.1:1"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if !called {
		t.Fatal("runner was not invoked")
	}
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("sync should redirect, got %d", rec.Code)
	}
}
