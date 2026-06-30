package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSettingsSavePersists(t *testing.T) {
	s := testServer(t)
	ctx := reqCtx()
	form := url.Values{
		"SHELFARR_URL":    {"http://192.168.1.89:5056"},
		"SHELVES":         {"to-read,sci-fi"},
		"SCHEDULE_PRESET": {"advanced"},
		"SCHEDULE_RAW":    {"0 */2 * * *"},
		"SHELFARR_TOKEN":  {""}, // empty secret -> unchanged
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

func TestSettingsTypedWidgets(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest("GET", "/settings", nil)
	req.RemoteAddr = "127.0.0.1:1"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /settings returned %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<select name="FORMAT"`,
		`<option value="ebook"`,
		`type="checkbox" name="LANG_INFERENCE"`,
		`type="number" name="MAX_REQUESTS_PER_RUN"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("settings page missing %q", want)
		}
	}
}

func TestSettingsCheckboxCanonicalOff(t *testing.T) {
	s := testServer(t)
	ctx := reqCtx()
	// Submit the form WITHOUT LANG_INFERENCE (unchecked) -> canonical off.
	form := url.Values{"SHELFARR_URL": {"http://192.168.1.89:5056"}}
	req := httptest.NewRequest("POST", "/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "127.0.0.1:1"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if v, _, _ := s.st.GetSetting(ctx, "LANG_INFERENCE"); v != "off" {
		t.Fatalf("unchecked checkbox must write canonical off, got %q", v)
	}
	// Submit WITH LANG_INFERENCE -> canonical on.
	form = url.Values{"LANG_INFERENCE": {"on"}}
	req = httptest.NewRequest("POST", "/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "127.0.0.1:1"
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if v, _, _ := s.st.GetSetting(ctx, "LANG_INFERENCE"); v != "on" {
		t.Fatalf("checked checkbox must write canonical on, got %q", v)
	}
}
