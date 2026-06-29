package shelfarr

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
)

func TestGetRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/requests/req_1" {
			w.Write([]byte(`{"status":"downloading","attention_needed":false,"issue_description":""}`))
			return
		}
		w.WriteHeader(404)
		w.Write([]byte(`{"error":"not found"}`))
	}))
	defer srv.Close()
	c := New(srv.URL, config.SecretString("t"), srv.Client())
	st, err := c.GetRequest(context.Background(), "req_1")
	if err != nil || st.Status != "downloading" {
		t.Fatalf("status=%+v err=%v", st, err)
	}
	if _, err := c.GetRequest(context.Background(), "gone"); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("want ErrRequestNotFound, got %v", err)
	}
}
