package shelfarr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
)

func TestSearchParsesResultsAndSendsAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer shf_t" {
			t.Errorf("missing/wrong auth header: %q", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/api/v1/search" || r.URL.Query().Get("q") != "dune" {
			t.Errorf("bad request: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		w.Write([]byte(`{"results":[
			{"work_id":"openlibrary:OL1W","title":"Dune","author":"Frank Herbert","year":1965,"confidence":90,"has_ebook":true},
			{"work_id":"google_books:abc","title":"Dune Messiah","author":"Frank Herbert","confidence":null}
		]}`))
	}))
	defer srv.Close()
	c := New(srv.URL, config.SecretString("shf_t"), srv.Client())
	res, err := c.Search(context.Background(), "dune", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0].WorkID != "openlibrary:OL1W" || *res[0].Confidence != 90 || !*res[0].HasEbook {
		t.Fatalf("bad parse: %+v", res)
	}
	if res[1].Confidence != nil {
		t.Fatalf("expected nil confidence, got %v", *res[1].Confidence)
	}
}
