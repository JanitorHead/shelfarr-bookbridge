package engine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/shelfarr"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/store"
)

func TestEngineUsesPerShelfFormat(t *testing.T) {
	var gotType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/search":
			w.Write([]byte(`{"results":[{"work_id":"ol:1","title":"Dune","author":"Frank Herbert","confidence":90}]}`))
		case strings.HasPrefix(r.URL.Path, "/api/v1/requests/"):
			w.Write([]byte(`{"status":"downloading"}`))
		case r.URL.Path == "/api/v1/requests":
			body, _ := io.ReadAll(r.Body)
			var m map[string]any
			json.Unmarshal(body, &m)
			if bt, _ := m["book_types"].([]any); len(bt) > 0 {
				gotType, _ = bt[0].(string)
			}
			w.WriteHeader(201)
			w.Write([]byte(`{"requests":[{"id":1}],"errors":[]}`))
		}
	}))
	defer srv.Close()
	st, _ := store.Open(t.TempDir() + "/bb.db")
	defer st.Close()
	ctx := context.Background()
	st.SetShelfConfig(ctx, "audio", true, "audiobook", "")
	sh := shelfarr.New(srv.URL, config.SecretString("t"), nil)
	src := fixedSource{[]sources.Book{{Source: "goodreads", ExternalID: "1", Title: "Dune", Author: "Frank Herbert", Shelves: []string{"audio"}}}}
	e := New(src, st, sh, config.Config{Format: "ebook", SimilarityThreshold: 0.82, MaxRequestsPerRun: 25, Shelves: []string{"audio"}})
	if _, err := e.Run(ctx, false); err != nil {
		t.Fatal(err)
	}
	if gotType != "audiobook" {
		t.Fatalf("per-shelf format override not applied, got %q", gotType)
	}
}
