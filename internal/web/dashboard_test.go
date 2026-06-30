package web

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

func TestDashboardShowsCounts(t *testing.T) {
	s := testServer(t)
	s.st.Diff(reqCtx(), []sources.Book{{Source: "goodreads", ExternalID: "1", Title: "A", Author: "X"}})
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "Dashboard") || !strings.Contains(body, "new") {
		t.Fatalf("dashboard missing counts: %s", body)
	}
}
