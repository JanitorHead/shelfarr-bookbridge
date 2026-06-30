package engine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/shelfarr"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/store"
)

type stubDetector struct{ lang string }

func (s stubDetector) Detect(string) (string, bool) {
	if s.lang == "" {
		return "", false
	}
	return s.lang, true
}

func TestEngineSendsDetectedLanguage(t *testing.T) {
	var gotLang string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/search" {
			w.Write([]byte(`{"results":[{"work_id":"ol:1","title":"Dune","author":"Frank Herbert","confidence":90}]}`))
			return
		}
		if r.Method == http.MethodGet { // reconcile poll: GET /api/v1/requests/:id
			w.Write([]byte(`{"status":"completed"}`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		json.Unmarshal(body, &m)
		gotLang, _ = m["language"].(string)
		w.WriteHeader(201)
		w.Write([]byte(`{"requests":[{"id":"req_1"}],"errors":[]}`))
	}))
	defer srv.Close()
	st, _ := store.Open(t.TempDir() + "/bb.db")
	defer st.Close()
	sh := shelfarr.New(srv.URL, config.SecretString("t"), nil)
	e := New(stubSrc(), st, sh, config.Config{Format: "ebook", SimilarityThreshold: 0.82, MaxRequestsPerRun: 25, LangInference: true, Shelves: []string{"to-read"}})
	e.SetDetector(stubDetector{lang: "es"})
	if _, err := e.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if gotLang != "es" {
		t.Fatalf("request language = %q, want es", gotLang)
	}
}

// TestEngineShelfLanguageOverridesInference proves a per-shelf language override
// wins over title inference — so a Spanish reader forces the Spanish edition even
// when the (English) title would be detected as English.
func TestEngineShelfLanguageOverridesInference(t *testing.T) {
	var gotLang string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/search" {
			w.Write([]byte(`{"results":[{"work_id":"ol:1","title":"Advice for a Young Investigator","author":"Ramon y Cajal","confidence":90}]}`))
			return
		}
		if r.Method == http.MethodGet {
			w.Write([]byte(`{"status":"completed"}`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		json.Unmarshal(body, &m)
		gotLang, _ = m["language"].(string)
		w.WriteHeader(201)
		w.Write([]byte(`{"requests":[{"id":"req_1"}],"errors":[]}`))
	}))
	defer srv.Close()
	st, _ := store.Open(t.TempDir() + "/bb.db")
	defer st.Close()
	ctx := context.Background()
	// The user shelves the English edition but configures the shelf as Spanish.
	st.SetShelfConfig(ctx, "to-read", true, "", "es")
	src := fixedSource{[]sources.Book{{Source: "goodreads", ExternalID: "1", Title: "Advice for a Young Investigator", Author: "Ramon y Cajal"}}}
	sh := shelfarr.New(srv.URL, config.SecretString("t"), nil)
	e := New(src, st, sh, config.Config{Format: "ebook", SimilarityThreshold: 0.82, MaxRequestsPerRun: 25, LangInference: true, Shelves: []string{"to-read"}})
	e.SetDetector(stubDetector{lang: "en"}) // inference would say English
	if _, err := e.Run(ctx, false); err != nil {
		t.Fatal(err)
	}
	if gotLang != "es" {
		t.Fatalf("request language = %q, want es (shelf override must beat inference)", gotLang)
	}
}

func stubSrc() sources.Source {
	return fixedSource{[]sources.Book{{Source: "goodreads", ExternalID: "1", Title: "Dune", Author: "Frank Herbert"}}}
}

type fixedSource struct{ b []sources.Book }

func (f fixedSource) Fetch(context.Context, []string) ([]sources.Book, error) {
	return withDefaultShelf(f.b), nil
}
