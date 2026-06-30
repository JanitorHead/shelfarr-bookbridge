package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/engine"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/store"
)

func testServer(t *testing.T) *Server {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st, func(bool) (engine.Report, error) { return engine.Report{}, nil })
}

func TestAuthLocalBypass(t *testing.T) {
	s := testServer(t)
	// default AUTH_REQUIRED=local: a private client reaches the dashboard
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.10:5000"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("local client should bypass auth, got %d", rec.Code)
	}
}

func TestAuthRedirectsExternal(t *testing.T) {
	s := testServer(t)
	s.st.SetSetting(reqCtx(), "AUTH_REQUIRED", "enabled")
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "8.8.8.8:5000"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "/login") {
		t.Fatalf("external client without session must redirect to /login, got %d loc=%s", rec.Code, rec.Header().Get("Location"))
	}
}

func TestSecurityHeaders(t *testing.T) {
	s := testServer(t)
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Header().Get("X-Frame-Options") != "DENY" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing security headers: %v", rec.Header())
	}
}

func reqCtx() context.Context { return context.Background() }
