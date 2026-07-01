package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/shelfarr"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/store"
)

// TestEngineAutoRetriesStuckRequest: a failed Shelfarr request, with auto-retry
// enabled and under the cap, is re-poked and kept in-flight (not marked failed).
func TestEngineAutoRetriesStuckRequest(t *testing.T) {
	retried := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/search":
			w.Write([]byte(`{"results":[{"work_id":"ol:1","title":"Dune","author":"Frank Herbert","confidence":90}]}`))
		case strings.HasSuffix(r.URL.Path, "/retry") && r.Method == http.MethodPost:
			retried = true
			w.Write([]byte(`{}`))
		case strings.HasPrefix(r.URL.Path, "/api/v1/requests/") && r.Method == http.MethodGet:
			w.Write([]byte(`{"status":"failed","attention_needed":false}`))
		case r.URL.Path == "/api/v1/requests":
			w.WriteHeader(201)
			w.Write([]byte(`{"requests":[{"id":"req_1"}],"errors":[]}`))
		}
	}))
	defer srv.Close()
	st, _ := store.Open(t.TempDir() + "/bb.db")
	defer st.Close()
	sh := shelfarr.New(srv.URL, config.SecretString("t"), nil)
	src := fixedSource{[]sources.Book{{Source: "goodreads", ExternalID: "1", Title: "Dune", Author: "Frank Herbert"}}}
	e := New(src, st, sh, config.Config{Format: "ebook", SimilarityThreshold: 0.82, MaxRequestsPerRun: 25,
		Shelves: []string{"to-read"}, ShelfarrAutoRetry: true, ShelfarrAutoRetryMax: 2})
	rep, err := e.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !retried {
		t.Fatal("expected Shelfarr retry to be called")
	}
	if rep.Retried != 1 {
		t.Fatalf("want retried=1, got %+v", rep)
	}
	var state string
	st.DB().QueryRow(`SELECT state FROM books WHERE external_id='1'`).Scan(&state)
	if state != "searching" {
		t.Fatalf("state=%q, want 'searching' (kept in-flight after retry)", state)
	}
	var n int
	st.DB().QueryRow(`SELECT shelfarr_retries FROM books WHERE external_id='1'`).Scan(&n)
	if n != 1 {
		t.Fatalf("shelfarr_retries=%d, want 1", n)
	}
}
