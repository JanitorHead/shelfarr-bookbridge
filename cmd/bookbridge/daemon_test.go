package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDaemonOnce(t *testing.T) {
	gr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<?xml version="1.0"?><rss><channel><item><book_id>1</book_id><title>Dune</title><author_name>Frank Herbert</author_name><isbn></isbn></item></channel></rss>`))
	}))
	defer gr.Close()
	sh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	defer sh.Close()
	env := map[string]string{
		"SHELFARR_URL": sh.URL, "SHELFARR_TOKEN": "shf_t",
		"GOODREADS_USER_ID": "42", "SHELVES": "to-read",
		"GOODREADS_BASE": gr.URL, "BB_DB": filepath.Join(t.TempDir(), "bb.db"),
	}
	var out strings.Builder
	code := run([]string{"daemon", "--once"}, func(k string) string { return env[k] }, &out)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "requested=1") {
		t.Fatalf("expected requested=1, got %s", out.String())
	}
}

func TestRunDaemonRefusesInsecureTransport(t *testing.T) {
	env := map[string]string{
		"SHELFARR_URL": "http://192.168.1.5:3000", "SHELFARR_TOKEN": "shf_t",
		"GOODREADS_USER_ID": "42", "SHELVES": "to-read",
		"BB_DB": filepath.Join(t.TempDir(), "bb.db"),
	}
	var out strings.Builder
	code := run([]string{"daemon", "--once"}, func(k string) string { return env[k] }, &out)
	if code == 0 {
		t.Fatalf("expected non-zero exit for insecure transport, out=%s", out.String())
	}
}
