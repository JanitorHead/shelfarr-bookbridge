package web

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSettingsSavePersists(t *testing.T) {
	s := testServer(t)
	ctx := reqCtx()
	form := url.Values{
		"SHELFARR_URL":   {"http://192.168.1.89:5056"},
		"SHELVES":        {"to-read,sci-fi"},
		"SCHEDULE":       {"0 */2 * * *"},
		"SHELFARR_TOKEN": {""}, // empty secret -> unchanged
	}
	req := httptest.NewRequest("POST", "/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "127.0.0.1:1" // local bypass (no csrf needed)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 303 && rec.Code != 200 {
		t.Fatalf("save returned %d", rec.Code)
	}
	if v, _, _ := s.st.GetSetting(ctx, "SCHEDULE"); v != "0 */2 * * *" {
		t.Fatalf("schedule not persisted: %q", v)
	}
	if _, ok, _ := s.st.GetSetting(ctx, "SHELFARR_TOKEN"); ok {
		t.Fatal("empty secret must not be written")
	}
}
