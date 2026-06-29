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

func (s stubSource) Fetch(context.Context, []string) ([]sources.Book, error) { return s.books, nil }

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
	cfg := config.Config{Format: "ebook", SimilarityThreshold: 0.82, MaxRequestsPerRun: 25}
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
