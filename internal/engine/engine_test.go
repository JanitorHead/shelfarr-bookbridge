package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/shelfarr"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/store"
)

type stubSource struct{ books []sources.Book }

func (s stubSource) Fetch(context.Context, []string) ([]sources.Book, error) {
	return withDefaultShelf(s.books), nil
}

// withDefaultShelf tags test books into the "to-read" download shelf when they
// have none, so they're promoted out of the catalog into the download queue.
func withDefaultShelf(books []sources.Book) []sources.Book {
	out := make([]sources.Book, len(books))
	for i, b := range books {
		if len(b.Shelves) == 0 {
			b.Shelves = []string{"to-read"}
		}
		out[i] = b
	}
	return out
}

func mockShelfarr(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/search" {
			w.Write([]byte(`{"results":[{"work_id":"ol:1","title":"Dune","author":"Frank Herbert","confidence":90}]}`))
			return
		}
		if r.URL.Path == "/api/v1/requests" {
			w.WriteHeader(201)
			w.Write([]byte(`{"requests":[{"id":"req_1"}],"errors":[]}`))
			return
		}
		if r.Method == http.MethodGet { // reconcile poll: GET /api/v1/requests/:id
			w.Write([]byte(`{"status":"completed"}`))
			return
		}
		t.Errorf("unexpected path %s", r.URL.Path)
	}))
}

func newEngine(t *testing.T, src sources.Source, base string) *Engine {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	sh := shelfarr.New(base, config.SecretString("shf_t"), nil)
	cfg := config.Config{Format: "ebook", SimilarityThreshold: 0.82, MaxRequestsPerRun: 25, Shelves: []string{"to-read"}}
	return New(src, st, sh, cfg)
}

func TestEngineApplyRequestsNewBook(t *testing.T) {
	srv := mockShelfarr(t)
	defer srv.Close()
	src := stubSource{books: []sources.Book{{Source: "goodreads", ExternalID: "1", Title: "Dune", Author: "Frank Herbert"}}}
	e := newEngine(t, src, srv.URL)
	rep, err := e.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.New != 1 || rep.Requested != 1 || rep.NotFound != 0 {
		t.Fatalf("bad report: %+v", rep)
	}
	// second run: already known -> nothing new
	rep2, _ := e.Run(context.Background(), false)
	if rep2.New != 0 || rep2.Requested != 0 {
		t.Fatalf("second run should be a no-op: %+v", rep2)
	}
}

func TestEngineDryRunRequestsNothing(t *testing.T) {
	srv := mockShelfarr(t)
	defer srv.Close()
	src := stubSource{books: []sources.Book{{Source: "goodreads", ExternalID: "1", Title: "Dune", Author: "Frank Herbert"}}}
	e := newEngine(t, src, srv.URL)
	rep, err := e.Run(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Requested != 0 || rep.New != 1 {
		t.Fatalf("dry-run must request nothing: %+v", rep)
	}
}
