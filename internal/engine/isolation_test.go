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

// A failing Shelfarr search for one book must NOT abort the whole run: the bad
// book is counted as an error and the others still get requested.
func TestEngineIsolatesPerBookSearchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/search":
			if strings.Contains(r.URL.RawQuery, "BADISBN") {
				w.WriteHeader(503) // simulate a slow/broken provider
				return
			}
			w.Write([]byte(`{"results":[{"work_id":"ol:1","title":"Dune","author":"Frank Herbert","confidence":90}]}`))
		case strings.HasPrefix(r.URL.Path, "/api/v1/requests/"):
			w.Write([]byte(`{"status":"downloading"}`))
		case r.URL.Path == "/api/v1/requests":
			w.WriteHeader(201)
			w.Write([]byte(`{"requests":[{"id":1}],"errors":[]}`))
		}
	}))
	defer srv.Close()

	st, err := store.Open(t.TempDir() + "/bb.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	sh := shelfarr.New(srv.URL, config.SecretString("t"), nil)
	books := []sources.Book{
		{Source: "goodreads", ExternalID: "1", Title: "Whatever", Author: "X", ISBN10: "BADISBN"},
		{Source: "goodreads", ExternalID: "2", Title: "Dune", Author: "Frank Herbert"},
	}
	e := New(fixedSource{books}, st, sh, config.Config{Format: "ebook", SimilarityThreshold: 0.82, MaxRequestsPerRun: 25})

	rep, err := e.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("run must not fail on a per-book error: %v", err)
	}
	if rep.Errors < 1 {
		t.Fatalf("want Errors>=1 for the broken search, got %+v", rep)
	}
	if rep.Requested != 1 {
		t.Fatalf("the healthy book must still be requested; got %+v", rep)
	}
}
