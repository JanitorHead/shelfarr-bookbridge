package goodreads

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
)

func TestRSSSourceFetchSendsKeyAndParses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/review/list_rss/42" {
			t.Errorf("bad path %s", r.URL.Path)
		}
		if r.URL.Query().Get("shelf") != "to-read" || r.URL.Query().Get("key") != "feedkey" {
			t.Errorf("bad query %s", r.URL.RawQuery)
		}
		w.Write([]byte(sampleRSS))
	}))
	defer srv.Close()
	s := NewRSSSource("42", config.SecretString("feedkey"), srv.URL, srv.Client())
	books, err := s.Fetch(context.Background(), []string{"to-read"})
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 2 {
		t.Fatalf("want 2, got %d", len(books))
	}
}
