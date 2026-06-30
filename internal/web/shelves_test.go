package web

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestShelvesSave(t *testing.T) {
	s := testServer(t)
	ctx := reqCtx()
	s.st.SetSetting(ctx, "SHELVES", "to-read,sci-fi")
	form := url.Values{
		"shelf":          {"sci-fi"},
		"enabled_sci-fi": {""}, // unchecked -> disabled
		"format_sci-fi":  {"audiobook"},
	}
	req := httptest.NewRequest("POST", "/shelves", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "127.0.0.1:1"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	en, _ := s.st.EnabledShelves(ctx, []string{"to-read", "sci-fi"})
	if len(en) != 1 || en[0] != "to-read" {
		t.Fatalf("sci-fi should be disabled: %v", en)
	}
	if f, ok := s.st.ShelfFormat(ctx, "sci-fi"); !ok || f != "audiobook" {
		t.Fatalf("format not saved: %q", f)
	}
}
