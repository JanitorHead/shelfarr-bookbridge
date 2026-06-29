package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSyncUsesCookieHTMLWhenCookieSet(t *testing.T) {
	var hitHTML bool
	gr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/review/list/") {
			hitHTML = true
			if r.URL.Query().Get("page") == "1" {
				w.Write([]byte(`<html><head><title>x</title></head><body><table><tbody id="booksBody"><tr><td class="field title"><div class="value"><a href="/book/show/12345.X">Dune</a></div></td><td class="field author"><div class="value"><a href="/author/show/1">Frank Herbert</a></div></td><td class="field isbn"><div class="value"></div></td></tr></tbody></table></body></html>`))
			} else {
				w.Write([]byte(`<html><head><title>x</title></head><body><table><tbody id="booksBody"></tbody></table></body></html>`))
			}
			return
		}
		w.Write([]byte(`{"results":[{"work_id":"ol:1","title":"Dune","author":"Frank Herbert","confidence":90}]}`))
	}))
	defer gr.Close()
	env := map[string]string{
		"SHELFARR_URL": gr.URL, "SHELFARR_TOKEN": "shf_t",
		"GOODREADS_USER_ID": "42", "SHELVES": "to-read",
		"GOODREADS_COOKIE": "sess=abc", "GOODREADS_BASE": gr.URL,
		"BB_DB": filepath.Join(t.TempDir(), "bb.db"),
	}
	var out strings.Builder
	code := run([]string{"sync", "--dry-run"}, func(k string) string { return env[k] }, &out)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if !hitHTML {
		t.Fatal("expected the cookie-HTML route to be used")
	}
	if !strings.Contains(out.String(), "new=1") {
		t.Fatalf("expected new=1, got %s", out.String())
	}
}
