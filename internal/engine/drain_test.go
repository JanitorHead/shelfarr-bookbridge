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

// A backlog larger than MAX_REQUESTS_PER_RUN must drain across successive runs:
// books not reached on run 1 (still state 'new') are requested on run 2, even
// though Diff returns nothing new the second time. Also exercises numeric
// Shelfarr request ids in both the create and status responses.
func TestEngineDrainsBacklogAcrossRuns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/search":
			w.Write([]byte(`{"results":[{"work_id":"ol:1","title":"Dune","author":"Frank Herbert","confidence":90}]}`))
		case strings.HasPrefix(r.URL.Path, "/api/v1/requests/"):
			w.Write([]byte(`{"id":777,"status":"downloading"}`)) // numeric id
		case r.URL.Path == "/api/v1/requests":
			w.WriteHeader(201)
			w.Write([]byte(`{"requests":[{"id":777}],"errors":[]}`)) // numeric id
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
		{Source: "goodreads", ExternalID: "1", Title: "Dune", Author: "Frank Herbert"},
		{Source: "goodreads", ExternalID: "2", Title: "Dune", Author: "Frank Herbert"},
		{Source: "goodreads", ExternalID: "3", Title: "Dune", Author: "Frank Herbert"},
	}
	e := New(fixedSource{books}, st, sh, config.Config{Format: "ebook", SimilarityThreshold: 0.82, MaxRequestsPerRun: 2, Shelves: []string{"to-read"}})

	rep1, err := e.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if rep1.New != 3 || rep1.Requested != 2 {
		t.Fatalf("run1: want New=3 Requested=2, got %+v", rep1)
	}
	rep2, err := e.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if rep2.New != 0 || rep2.Requested != 1 {
		t.Fatalf("run2: want New=0 Requested=1 (drains remainder), got %+v", rep2)
	}
}
