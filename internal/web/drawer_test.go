package web

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

// TestDrawerDoesNotPoisonTemplate reproduces the "cannot Clone after execute"
// bug: opening a book drawer must NOT break subsequent page renders.
func TestDrawerDoesNotPoisonTemplate(t *testing.T) {
	s := testServer(t)
	s.st.Diff(reqCtx(), []sources.Book{{Source: "goodreads", ExternalID: "1", Title: "Dune", Author: "Herbert"}})

	// Open the drawer (this used to execute the shared template directly).
	req := httptest.NewRequest("GET", "/book/goodreads/1?drawer=1", nil)
	req.RemoteAddr = "127.0.0.1:1"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "Dune") {
		t.Fatalf("drawer partial missing: %s", rec.Body.String())
	}

	// Now every normal page must still render (clone must still work).
	for _, path := range []string{"/", "/activity", "/settings"} {
		r := httptest.NewRequest("GET", path, nil)
		r.RemoteAddr = "127.0.0.1:1"
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 200 || strings.Contains(w.Body.String(), "cannot Clone") {
			t.Fatalf("%s broke after drawer: code=%d body=%.120s", path, w.Code, w.Body.String())
		}
	}
}
