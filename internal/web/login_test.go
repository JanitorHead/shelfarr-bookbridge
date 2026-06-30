package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/auth"
)

func TestLoginFlow(t *testing.T) {
	s := testServer(t)
	ctx := reqCtx()
	s.st.SetSetting(ctx, "AUTH_USERNAME", "admin")
	h, _ := auth.Hash("secret")
	s.st.SetSetting(ctx, "AUTH_PASSWORD_HASH", h)
	s.st.SetSetting(ctx, "AUTH_REQUIRED", "enabled")

	// wrong password -> no session cookie
	form := url.Values{"username": {"admin"}, "password": {"nope"}}
	req := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "8.8.8.8:1"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if strings.Contains(rec.Header().Get("Set-Cookie"), "bb_session") {
		t.Fatal("wrong password must not set a session")
	}

	// right password -> session cookie + redirect home
	form.Set("password", "secret")
	req = httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "8.8.8.8:1"
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Set-Cookie"), "bb_session") {
		t.Fatalf("valid login should set session + redirect, got %d %q", rec.Code, rec.Header().Get("Set-Cookie"))
	}
}
