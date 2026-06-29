package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSyncDryRun(t *testing.T) {
	gr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<?xml version="1.0"?><rss><channel><item><book_id>1</book_id><title>Dune</title><author_name>Frank Herbert</author_name><isbn></isbn></item></channel></rss>`))
	}))
	defer gr.Close()
	sh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"results":[{"work_id":"ol:1","title":"Dune","author":"Frank Herbert","confidence":90}]}`))
	}))
	defer sh.Close()

	env := map[string]string{
		"SHELFARR_URL": sh.URL, "SHELFARR_TOKEN": "shf_t",
		"GOODREADS_USER_ID": "42", "SHELVES": "to-read",
		"BB_DB":             filepath.Join(t.TempDir(), "bb.db"),
		"GOODREADS_BASE":    gr.URL,
	}
	var out strings.Builder
	code := run([]string{"sync", "--dry-run"}, func(k string) string { return env[k] }, &out)
	if code != 0 {
		t.Fatalf("exit %d, out=%s", code, out.String())
	}
	if !strings.Contains(out.String(), "new=1") {
		t.Fatalf("expected new=1 in output, got %s", out.String())
	}
}
