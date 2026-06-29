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

func TestEngineReconcileMarksCompleted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/search":
			w.Write([]byte(`{"results":[{"work_id":"ol:1","title":"Dune","author":"Frank Herbert","confidence":90}]}`))
		case strings.HasPrefix(r.URL.Path, "/api/v1/requests/"):
			w.Write([]byte(`{"status":"completed"}`))
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
	e := New(src, st, sh, config.Config{Format: "ebook", SimilarityThreshold: 0.82, MaxRequestsPerRun: 25})
	rep, err := e.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Requested != 1 {
		t.Fatalf("want 1 requested, got %+v", rep)
	}
	if rep.Reconciled != 1 || rep.Completed != 1 {
		t.Fatalf("want reconciled=1 completed=1, got %+v", rep)
	}
	var state string
	st.DB().QueryRow(`SELECT state FROM books WHERE external_id='1'`).Scan(&state)
	if state != "done" {
		t.Fatalf("state=%q want done", state)
	}
}
