package goodreads

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
)

func TestHTMLSourcePaginatesUntilEmpty(t *testing.T) {
	var gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		page := r.URL.Query().Get("page")
		switch page {
		case "1":
			w.Write([]byte(sampleHTML)) // 2 books
		default:
			w.Write([]byte(`<html><head><title>x</title></head><body><tbody id="booksBody"></tbody><div class="greyText nocontent stacked">no books</div></body></html>`))
		}
	}))
	defer srv.Close()
	s := NewHTMLSource("42", config.SecretString("sess=abc"), srv.URL, srv.Client())
	books, err := s.Fetch(context.Background(), []string{"to-read"})
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 2 {
		t.Fatalf("want 2, got %d", len(books))
	}
	if !strings.Contains(gotCookie, "sess=abc") {
		t.Fatalf("cookie not sent: %q", gotCookie)
	}
}

func TestHTMLSourceCookieExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, signinHTML)
	}))
	defer srv.Close()
	s := NewHTMLSource("42", config.SecretString("sess=stale"), srv.URL, srv.Client())
	_, err := s.Fetch(context.Background(), []string{"to-read"})
	if !errors.Is(err, ErrCookieExpired) {
		t.Fatalf("want ErrCookieExpired, got %v", err)
	}
}
