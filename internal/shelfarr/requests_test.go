package shelfarr

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
)

func TestCreateRequestSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		json.Unmarshal(body, &got)
		if got["work_id"] != "openlibrary:OL1W" {
			t.Errorf("bad work_id: %v", got["work_id"])
		}
		if bt, _ := got["book_types"].([]any); len(bt) != 1 || bt[0] != "ebook" {
			t.Errorf("bad book_types: %v", got["book_types"])
		}
		w.WriteHeader(201)
		w.Write([]byte(`{"requests":[{"id":"req_7"}],"warnings":[],"errors":[]}`))
	}))
	defer srv.Close()
	c := New(srv.URL, config.SecretString("shf_t"), srv.Client())
	id, exists, err := c.CreateRequest(context.Background(), CreateRequestParams{
		WorkID: "openlibrary:OL1W", BookTypes: []string{"ebook"}, Title: "Dune",
	})
	if err != nil || exists || id != "req_7" {
		t.Fatalf("id=%q exists=%v err=%v", id, exists, err)
	}
}

func TestCreateRequestDuplicate422(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		w.Write([]byte(`{"errors":["This book already has an active request"]}`))
	}))
	defer srv.Close()
	c := New(srv.URL, config.SecretString("shf_t"), srv.Client())
	_, exists, err := c.CreateRequest(context.Background(), CreateRequestParams{WorkID: "x", BookTypes: []string{"ebook"}})
	if err != nil {
		t.Fatalf("422-already-exists must not be an error: %v", err)
	}
	if !exists {
		t.Fatal("expected alreadyExists=true on 422")
	}
}
