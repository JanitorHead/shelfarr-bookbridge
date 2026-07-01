package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/store"
)

func TestDashboardShowsCounts(t *testing.T) {
	s := testServer(t)
	s.st.Diff(reqCtx(), []sources.Book{{Source: "goodreads", ExternalID: "1", Title: "A", Author: "X"}})
	req := httptest.NewRequest("GET", "/activity", nil)
	req.RemoteAddr = "127.0.0.1:1"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "Activity") || !strings.Contains(body, "catalog") {
		t.Fatalf("dashboard missing counts: %s", body)
	}
}

func TestDashboardShowsRunStatus(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest("GET", "/activity", nil)
	req.RemoteAddr = "127.0.0.1:1"

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "Idle") {
		t.Fatalf("dashboard should show Idle status badge: %s", body)
	}
	if !strings.Contains(body, "Next run") {
		t.Fatalf("dashboard should show Next run line: %s", body)
	}

	start := time.Now().Add(-2 * time.Second)
	if _, err := s.st.RecordRun(reqCtx(), store.RunRecord{
		StartedAt: start, FinishedAt: time.Now(), Mode: "dry-run", OK: true,
		Fetched: 5, Requested: 2, Summary: "[dry-run] fetched=5 requested=2",
	}); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	body = rec.Body.String()
	if !strings.Contains(body, "dry-run") {
		t.Fatalf("dashboard should show the last run mode: %s", body)
	}
	if !strings.Contains(body, "OK") {
		t.Fatalf("dashboard should show an OK marker for a successful run: %s", body)
	}
}
