package web

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

func TestQueueListsBooks(t *testing.T) {
	s := testServer(t)
	s.st.Diff(reqCtx(), []sources.Book{{Source: "goodreads", ExternalID: "1", Title: "Dune", Author: "Frank Herbert"}})
	req := httptest.NewRequest("GET", "/queue", nil)
	req.RemoteAddr = "127.0.0.1:1"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "Dune") {
		t.Fatalf("queue missing book: %s", rec.Body.String())
	}
}

func TestReviewIgnoreAction(t *testing.T) {
	s := testServer(t)
	ctx := reqCtx()
	s.st.Diff(ctx, []sources.Book{{Source: "goodreads", ExternalID: "1", Title: "Z", Author: "Q"}})
	s.st.SetState(ctx, sources.Book{Source: "goodreads", ExternalID: "1"}, "not_found")
	form := url.Values{"action": {"ignore"}, "source": {"goodreads"}, "external_id": {"1"}}
	req := httptest.NewRequest("POST", "/review", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "127.0.0.1:1"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	rows, _ := s.st.ListBooks(ctx, "ignored", 10)
	if len(rows) != 1 {
		t.Fatalf("ignore action did not apply: %+v", rows)
	}
}
