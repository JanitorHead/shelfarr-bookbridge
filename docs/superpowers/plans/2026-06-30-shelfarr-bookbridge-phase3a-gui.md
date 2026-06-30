# Shelfarr BookBridge — Phase 3a: Web GUI Core (settings, auth, dashboard) + Docker

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Goal:** A self-hosted web GUI (one Docker container on Unraid) to log in, edit every parameter (persisted), see status, and trigger a sync — with *arr-style auth (Forms login, optionally disabled for local addresses).

**Architecture:** Settings live in SQLite and override env (env only seeds). The existing `config.loadFrom` is reused by feeding it a getenv that prefers stored settings. A `net/http` server (embedded `html/template` + CSS) adds security headers and an auth middleware that mirrors Sonarr/Radarr: method `forms`/`none`, and "authentication required" = `enabled` or `local` (bypassed for private/loopback clients). The daemon serves the GUI alongside the scheduler; "Sync now" rebuilds the engine from effective config so GUI edits take effect.

**Tech Stack:** Go 1.23+, stdlib `net/http`/`html/template`, `golang.org/x/crypto/argon2` (fetched), existing stack. No JS framework.

## Global Constraints

- Module `github.com/JanitorHead/shelfarr-bookbridge`; `SecretString`; TDD; `go test ./...` green per task; commit per task with `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>` trailer.
- **Settings keys are env-var names** (e.g. `SHELFARR_URL`, `SCHEDULE`). Stored values override env via a merged getenv; this reuses ALL existing parsing in `config.loadFrom`.
- Secrets (`SHELFARR_TOKEN`, `GOODREADS_COOKIE`, `GOODREADS_FEED_KEY`) are **write-only** in the GUI: shown as a "set/not set" indicator, never echoed; only overwritten when a new value is submitted.
- Auth: `AUTH_METHOD` ∈ `forms|none` (default `forms`); `AUTH_REQUIRED` ∈ `enabled|local` (default `local` = bypass auth for private/loopback clients, like *arr's "Disabled for Local Addresses"). Admin creds stored as `AUTH_USERNAME` + `AUTH_PASSWORD_HASH` (argon2id); bootstrapped from env `AUTH_USERNAME`(default `admin`)/`AUTH_PASSWORD`.
- GUI binds `GUI_BIND` (default `0.0.0.0`) on `GUI_PORT` (default `7373`).

---

## File Structure

| Path | Responsibility | Change |
|---|---|---|
| `internal/store/store.go` | schema v4 `settings` table | Modify |
| `internal/store/settings.go` | GetSetting/SetSetting/AllSettings + StateCounts | Create |
| `internal/config/config.go` | GUI/auth fields + `LoadEffective` | Modify |
| `internal/auth/auth.go` | argon2id hash/verify, local-IP check | Create |
| `internal/web/server.go` | Server, sessions, middleware, render | Create |
| `internal/web/templates/*.html` | base + login + dashboard + settings | Create |
| `internal/web/static/style.css` | minimal dark CSS | Create |
| `internal/web/handlers.go` | login/logout/dashboard/settings/actions | Create |
| `cmd/bookbridge/main.go` | `web` cmd + serve GUI in daemon + effective cfg | Modify |
| `Dockerfile`,`docker-compose.yml`,`unraid-template.xml` | expose 7373 + auth env | Modify |

---

### Task 1: Settings table + store accessors

**Files:** Modify `internal/store/store.go`; Create `internal/store/settings.go`, `internal/store/settings_test.go`

**Interfaces:** schema v4 `settings(key PK, value)`; `(*Store).GetSetting(ctx,key)(string,bool,error)`, `SetSetting(ctx,key,val)error`, `AllSettings(ctx)(map[string]string,error)`, `StateCounts(ctx)(map[string]int,error)`.

- [ ] **Step 1: Failing test** — Create `internal/store/settings_test.go`:
```go
package store

import (
	"context"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

func TestSettingsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, ok, _ := s.GetSetting(ctx, "SCHEDULE"); ok {
		t.Fatal("unset key should report ok=false")
	}
	if err := s.SetSetting(ctx, "SCHEDULE", "*/30 * * * *"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting(ctx, "SCHEDULE", "0 * * * *"); err != nil { // upsert
		t.Fatal(err)
	}
	v, ok, _ := s.GetSetting(ctx, "SCHEDULE")
	if !ok || v != "0 * * * *" {
		t.Fatalf("got %q ok=%v", v, ok)
	}
	all, _ := s.AllSettings(ctx)
	if all["SCHEDULE"] != "0 * * * *" {
		t.Fatalf("AllSettings: %v", all)
	}
}

func TestStateCounts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	s.Diff(ctx, []sources.Book{
		{Source: "goodreads", ExternalID: "1", Title: "A", Author: "X"},
		{Source: "goodreads", ExternalID: "2", Title: "B", Author: "Y"},
	})
	s.SetState(ctx, sources.Book{Source: "goodreads", ExternalID: "1"}, "requested")
	c, _ := s.StateCounts(ctx)
	if c["new"] != 1 || c["requested"] != 1 {
		t.Fatalf("counts: %v", c)
	}
}
```

- [ ] **Step 2: Run — must fail.** `export PATH="/c/Program Files/Go/bin:$PATH" && cd /c/source/repo/shelfarr-bookbridge && go test ./internal/store/ -run "TestSettings|TestStateCounts" -v` → FAIL.

- [ ] **Step 3: Schema v4.** In `internal/store/store.go`, change `const schemaVersion = 3` to `4` and append to the `migrations` slice (after v3):
```go
		// v4
		`CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);`,
```

- [ ] **Step 4: Accessors.** Create `internal/store/settings.go`:
```go
package store

import "context"

func (s *Store) GetSetting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return "", false, nil
		}
		return "", false, err
	}
	return v, true, nil
}

func (s *Store) SetSetting(ctx context.Context, key, val string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO settings(key,value) VALUES(?,?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, val)
	return err
}

func (s *Store) AllSettings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

func (s *Store) StateCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT state, COUNT(*) FROM books GROUP BY state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			return nil, err
		}
		out[st] = n
	}
	return out, rows.Err()
}
```
Use `errors.Is(err, sql.ErrNoRows)` instead of string compare — add imports `database/sql` and `errors`, and replace the `err.Error() == ...` check with `if errors.Is(err, sql.ErrNoRows) { return "", false, nil }`.

- [ ] **Step 5: Run + commit.** `go test ./internal/store/ -v` then `go test ./...` → green.
```bash
git add internal/store/store.go internal/store/settings.go internal/store/settings_test.go
git commit -m "feat(store): settings table + StateCounts (schema v4)"
```

### Task 2: GUI/auth config fields + LoadEffective

**Files:** Modify `internal/config/config.go`; Create `internal/config/effective_test.go`

**Interfaces:** `Config` gains `GUIPort, GUIBind, AuthMethod, AuthRequired string`; `config.LoadEffective(getenv func(string)string, settings map[string]string)(Config,error)`.

- [ ] **Step 1: Failing test** — Create `internal/config/effective_test.go`:
```go
package config

import "testing"

func TestLoadEffectiveStoreOverridesEnv(t *testing.T) {
	env := map[string]string{"SHELFARR_URL": "http://env", "SHELFARR_TOKEN": "t", "SCHEDULE": "0 * * * *"}
	settings := map[string]string{"SCHEDULE": "*/15 * * * *", "GUI_PORT": "9000"}
	c, err := LoadEffective(func(k string) string { return env[k] }, settings)
	if err != nil {
		t.Fatal(err)
	}
	if c.Schedule != "*/15 * * * *" {
		t.Fatalf("store should override env schedule, got %q", c.Schedule)
	}
	if c.ShelfarrURL != "http://env" {
		t.Fatalf("env should apply when unset in store, got %q", c.ShelfarrURL)
	}
	if c.GUIPort != "9000" {
		t.Fatalf("GUIPort: %q", c.GUIPort)
	}
}

func TestGUIDefaults(t *testing.T) {
	c, _ := loadFrom(func(k string) string {
		return map[string]string{"SHELFARR_URL": "u", "SHELFARR_TOKEN": "t"}[k]
	})
	if c.GUIPort != "7373" || c.GUIBind != "0.0.0.0" || c.AuthMethod != "forms" || c.AuthRequired != "local" {
		t.Fatalf("gui/auth defaults wrong: %+v", c)
	}
}
```

- [ ] **Step 2: Run — must fail.** `go test ./internal/config/ -run "TestLoadEffective|TestGUIDefaults" -v` → FAIL.

- [ ] **Step 3: Add fields + defaults.** In `internal/config/config.go`, add to the `Config` struct:
```go
	GUIPort      string
	GUIBind      string
	AuthMethod   string
	AuthRequired string
```
and in `loadFrom`'s `Config{...}` literal:
```go
		GUIPort:      orDefault(get("GUI_PORT"), "7373"),
		GUIBind:      orDefault(get("GUI_BIND"), "0.0.0.0"),
		AuthMethod:   orDefault(get("AUTH_METHOD"), "forms"),
		AuthRequired: orDefault(get("AUTH_REQUIRED"), "local"),
```

- [ ] **Step 4: LoadEffective.** Append to `internal/config/config.go`:
```go
// LoadEffective overlays stored settings (keyed by env-var name) over the
// environment, then parses as usual — so GUI-edited settings win over env.
func LoadEffective(getenv func(string) string, settings map[string]string) (Config, error) {
	merged := func(k string) string {
		if v, ok := settings[k]; ok && v != "" {
			return v
		}
		return getenv(k)
	}
	return loadFrom(merged)
}
```

- [ ] **Step 5: Run + commit.** `go test ./internal/config/ -v` then `go test ./...`.
```bash
git add internal/config/config.go internal/config/effective_test.go
git commit -m "feat(config): GUI/auth fields + LoadEffective (store overrides env)"
```

### Task 3: Auth primitives

**Files:** Create `internal/auth/auth.go`, `internal/auth/auth_test.go`

**Interfaces:** `auth.Hash(password string)(string,error)`, `auth.Verify(encoded, password string) bool`, `auth.IsLocalAddr(remoteAddr string) bool`.

- [ ] **Step 1: Failing test** — Create `internal/auth/auth_test.go`:
```go
package auth

import "testing"

func TestHashVerify(t *testing.T) {
	h, err := Hash("hunter2")
	if err != nil {
		t.Fatal(err)
	}
	if !Verify(h, "hunter2") {
		t.Fatal("correct password should verify")
	}
	if Verify(h, "wrong") {
		t.Fatal("wrong password must not verify")
	}
	if Verify("garbage", "hunter2") {
		t.Fatal("malformed hash must not verify")
	}
}

func TestIsLocalAddr(t *testing.T) {
	for _, a := range []string{"127.0.0.1:54321", "192.168.1.5:80", "10.0.0.2:1", "[::1]:7373", "172.16.3.4:9"} {
		if !IsLocalAddr(a) {
			t.Errorf("%s should be local", a)
		}
	}
	for _, a := range []string{"8.8.8.8:443", "203.0.113.7:80"} {
		if IsLocalAddr(a) {
			t.Errorf("%s should NOT be local", a)
		}
	}
}
```

- [ ] **Step 2: Run — must fail.** `go test ./internal/auth/ -v` → FAIL.

- [ ] **Step 3: Implement.** Create `internal/auth/auth.go`:
```go
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    = 1
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonKeyLen  = 32
)

// Hash returns an encoded argon2id hash: "argon2id$<saltB64>$<hashB64>".
func Hash(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("argon2id$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// Verify checks a password against an encoded hash in constant time.
func Verify(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 3 || parts[0] != "argon2id" {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[1])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[2])
	if err1 != nil || err2 != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// IsLocalAddr reports whether a request RemoteAddr is loopback or RFC1918/ULA.
func IsLocalAddr(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}
```

- [ ] **Step 4: Run + commit.** `go test ./internal/auth/ -v` then `go test ./...`.
```bash
git add internal/auth/
git commit -m "feat(auth): argon2id hashing + local-address detection"
```

### Task 4: Web server skeleton (sessions, middleware, render)

**Files:** Create `internal/web/server.go`, `internal/web/templates/base.html`, `internal/web/static/style.css`, `internal/web/server_test.go`

**Interfaces:** `web.New(st *store.Store, runner Runner) *Server`; `Runner = func(dryRun bool)(engine.Report,error)`; `(*Server).Handler() http.Handler`. Server reads effective config via `st` each request. Middleware: security headers + auth (method/required/local). Sessions in-memory; cookie `bb_session`; per-session CSRF token.

- [ ] **Step 1: Failing test** — Create `internal/web/server_test.go`:
```go
package web

import (
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
```
Add a tiny helper at the bottom of the test file:
```go
func reqCtx() context.Context { return context.Background() }
```
(import `context`).

- [ ] **Step 2: Run — must fail.** `go test ./internal/web/ -v` → FAIL.

- [ ] **Step 3: Base template + CSS.** Create `internal/web/templates/base.html`:
```html
{{define "base"}}<!doctype html><html lang="en"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>BookBridge — {{.Title}}</title><link rel="stylesheet" href="/static/style.css"></head>
<body><header><a class="brand" href="/">📚 BookBridge</a>
{{if .Authed}}<nav><a href="/">Dashboard</a><a href="/settings">Settings</a><a href="/shelves">Shelves</a><a href="/queue">Queue</a><a href="/review">Review</a><form method="post" action="/logout" class="inline"><input type="hidden" name="csrf" value="{{.CSRF}}"><button>Logout</button></form></nav>{{end}}
</header><main>{{if .Flash}}<div class="flash">{{.Flash}}</div>{{end}}{{template "content" .}}</main></body></html>{{end}}
```
Create `internal/web/static/style.css`:
```css
:root{color-scheme:dark}body{font-family:system-ui,sans-serif;margin:0;background:#1b1f23;color:#e6e6e6}
header{display:flex;gap:1rem;align-items:center;padding:.8rem 1.2rem;background:#23282d;border-bottom:1px solid #333}
.brand{font-weight:700;text-decoration:none;color:#fff}nav{display:flex;gap:1rem;align-items:center;margin-left:auto}
nav a{color:#9ad;text-decoration:none}main{max-width:920px;margin:1.5rem auto;padding:0 1rem}
.flash{background:#2d4a2d;border:1px solid #3a6;padding:.6rem 1rem;border-radius:6px;margin-bottom:1rem}
table{width:100%;border-collapse:collapse}th,td{text-align:left;padding:.4rem .6rem;border-bottom:1px solid #333}
label{display:block;margin:.6rem 0 .2rem;font-size:.9rem;color:#bbb}input,select{width:100%;max-width:480px;padding:.45rem;background:#15181b;color:#eee;border:1px solid #444;border-radius:5px}
button{background:#2b6cb0;color:#fff;border:0;padding:.5rem .9rem;border-radius:5px;cursor:pointer;margin-top:.8rem}
.inline{display:inline;margin:0}.cards{display:flex;gap:1rem;flex-wrap:wrap}.card{background:#23282d;border:1px solid #333;border-radius:8px;padding:1rem 1.4rem;min-width:120px}
.card .n{font-size:1.8rem;font-weight:700}.muted{color:#888;font-size:.85rem}.set{color:#3a6}.unset{color:#c66}
```

- [ ] **Step 4: Server.** Create `internal/web/server.go`:
```go
package web

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"html/template"
	"net/http"
	"sync"
	"time"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/auth"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/engine"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/store"
)

//go:embed templates/*.html
var tmplFS embed.FS

//go:embed static/*
var staticFS embed.FS

// Runner builds the engine from effective config and runs it.
type Runner func(dryRun bool) (engine.Report, error)

type session struct {
	user    string
	csrf    string
	expires time.Time
}

type Server struct {
	st     *store.Store
	run    Runner
	tmpl   *template.Template
	mu     sync.Mutex
	sess   map[string]*session
}

func New(st *store.Store, run Runner) *Server {
	t := template.Must(template.ParseFS(tmplFS, "templates/*.html"))
	return &Server{st: st, run: run, tmpl: t, sess: map[string]*session{}}
}

func (s *Server) cfg() config.Config {
	all, _ := s.st.AllSettings(context.Background())
	c, _ := config.LoadEffective(emptyEnv, all) // GUI process: env not used for effective reads
	return c
}

func emptyEnv(string) string { return "" }

func token() string {
	b := make([]byte, 24)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *Server) newSession(user string) (string, *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := token()
	se := &session{user: user, csrf: token(), expires: time.Now().Add(7 * 24 * time.Hour)}
	s.sess[id] = se
	return id, se
}

func (s *Server) session(r *http.Request) *session {
	c, err := r.Cookie("bb_session")
	if err != nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	se := s.sess[c.Value]
	if se == nil || time.Now().After(se.expires) {
		return nil
	}
	return se
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/static/", http.FileServer(http.FS(staticFS)))
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/logout", s.handleLogout)
	mux.HandleFunc("/settings", s.guard(s.handleSettings))
	mux.HandleFunc("/actions/sync", s.guard(s.handleSync))
	mux.HandleFunc("/", s.guard(s.handleDashboard))
	return securityHeaders(mux)
}

func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		h.ServeHTTP(w, r)
	})
}

// guard enforces auth per the *arr model.
func (s *Server) guard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := s.cfg()
		authed := s.session(r) != nil
		if c.AuthMethod == "none" || (c.AuthRequired == "local" && auth.IsLocalAddr(r.RemoteAddr)) {
			h(w, r)
			return
		}
		if !authed {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		h(w, r)
	}
}

// render executes the base template with a page-specific "content" block.
func (s *Server) render(w http.ResponseWriter, r *http.Request, page, title string, data map[string]any) {
	se := s.session(r)
	base := map[string]any{"Title": title, "Authed": se != nil, "CSRF": "", "Flash": ""}
	if se != nil {
		base["CSRF"] = se.csrf
	}
	for k, v := range data {
		base[k] = v
	}
	t, err := s.tmpl.Clone()
	if err == nil {
		_, err = t.ParseFS(tmplFS, "templates/"+page+".html")
	}
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "base", base); err != nil {
		http.Error(w, err.Error(), 500)
	}
}
```

> Note: `handleLogin`, `handleLogout`, `handleSettings`, `handleSync`, `handleDashboard` are added in Tasks 5–8. To make THIS task compile and pass, also create a temporary `internal/web/handlers_stub.go` with no-op stubs that the later tasks REPLACE:
```go
package web

import "net/http"

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request)     { s.render(w, r, "login", "Login", nil) }
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request)    { http.Redirect(w, r, "/login", http.StatusSeeOther) }
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) { s.render(w, r, "dashboard", "Dashboard", nil) }
func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request)  { s.render(w, r, "settings", "Settings", nil) }
func (s *Server) handleSync(w http.ResponseWriter, r *http.Request)      { http.Redirect(w, r, "/", http.StatusSeeOther) }
```
Create minimal `templates/login.html`, `templates/dashboard.html`, `templates/settings.html` each as `{{define "content"}}<p>{{.Title}}</p>{{end}}` so render works; Tasks 5–8 flesh them out.

- [ ] **Step 5: Run + commit.** `go test ./internal/web/ -v` then `go test ./...` → green.
```bash
git add internal/web/ 
git commit -m "feat(web): server skeleton — sessions, auth middleware (arr-style), security headers"
```

### Task 5: Login / logout

**Files:** Modify `internal/web/handlers_stub.go` → split into `internal/web/handlers.go` (remove the login/logout stubs from the stub file); Modify `internal/web/templates/login.html`; Create `internal/web/login_test.go`

**Interfaces:** GET `/login` renders a form; POST `/login` verifies `AUTH_USERNAME`/`AUTH_PASSWORD_HASH` (or, if no hash is set, first POST creates the admin from the submitted credentials), sets `bb_session`; POST `/logout` clears it. CSRF enforced on POST.

- [ ] **Step 1: Failing test** — Create `internal/web/login_test.go`:
```go
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
```

- [ ] **Step 2: Run — must fail.** `go test ./internal/web/ -run TestLoginFlow -v` → FAIL (stub login doesn't authenticate).

- [ ] **Step 3: Implement.** Create `internal/web/handlers.go`:
```go
package web

import (
	"context"
	"net/http"
	"time"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/auth"
)

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	if r.Method == http.MethodPost {
		r.ParseForm()
		user := r.PostFormValue("username")
		pass := r.PostFormValue("password")
		storedUser, _, _ := s.st.GetSetting(ctx, "AUTH_USERNAME")
		hash, hasHash, _ := s.st.GetSetting(ctx, "AUTH_PASSWORD_HASH")
		if storedUser == "" {
			storedUser = "admin"
		}
		ok := false
		if !hasHash || hash == "" {
			// first-run: create the admin from the first submitted credentials
			if user != "" && pass != "" {
				h, _ := auth.Hash(pass)
				s.st.SetSetting(ctx, "AUTH_USERNAME", user)
				s.st.SetSetting(ctx, "AUTH_PASSWORD_HASH", h)
				ok = true
				storedUser = user
			}
		} else {
			ok = user == storedUser && auth.Verify(hash, pass)
		}
		if !ok {
			s.render(w, r, "login", "Login", map[string]any{"Error": "Invalid credentials"})
			return
		}
		id, _ := s.newSession(storedUser)
		http.SetCookie(w, &http.Cookie{Name: "bb_session", Value: id, Path: "/", HttpOnly: true,
			SameSite: http.SameSiteStrictMode, Expires: time.Now().Add(7 * 24 * time.Hour)})
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.render(w, r, "login", "Login", nil)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("bb_session"); err == nil {
		s.mu.Lock()
		delete(s.sess, c.Value)
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "bb_session", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// requireCSRF returns false (and writes 403) if the POST lacks a valid token.
func (s *Server) requireCSRF(w http.ResponseWriter, r *http.Request) bool {
	se := s.session(r)
	if se == nil || r.PostFormValue("csrf") != se.csrf {
		http.Error(w, "bad csrf token", http.StatusForbidden)
		return false
	}
	return true
}
```
Remove the `handleLogin` and `handleLogout` stubs from `internal/web/handlers_stub.go` (keep the dashboard/settings/sync stubs there for now).

- [ ] **Step 4: Login template.** Replace `internal/web/templates/login.html`:
```html
{{define "content"}}<h1>Sign in</h1>
{{if .Error}}<div class="flash" style="background:#4a2d2d;border-color:#a33">{{.Error}}</div>{{end}}
<form method="post" action="/login">
<label>Username</label><input name="username" autofocus>
<label>Password</label><input name="password" type="password">
<button type="submit">Sign in</button></form>
<p class="muted">First sign-in sets the admin credentials.</p>{{end}}
```

- [ ] **Step 5: Run + commit.** `go test ./internal/web/ -v` then `go test ./...`.
```bash
git add internal/web/
git commit -m "feat(web): forms login/logout with first-run admin bootstrap + CSRF helper"
```

### Task 6: Dashboard

**Files:** Modify `internal/web/handlers.go` (add real `handleDashboard`, remove its stub); Modify `internal/web/templates/dashboard.html`; Create `internal/web/dashboard_test.go`

**Interfaces:** `/` renders state counts (`store.StateCounts`), the last-run summary (setting `LAST_RUN`), and a Goodreads-auth banner; `Sync now`/`Dry run` buttons.

- [ ] **Step 1: Failing test** — Create `internal/web/dashboard_test.go`:
```go
package web

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

func TestDashboardShowsCounts(t *testing.T) {
	s := testServer(t)
	s.st.Diff(reqCtx(), []sources.Book{{Source: "goodreads", ExternalID: "1", Title: "A", Author: "X"}})
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "Dashboard") || !strings.Contains(body, "new") {
		t.Fatalf("dashboard missing counts: %s", body)
	}
}
```

- [ ] **Step 2: Run — must fail (or render stub).** `go test ./internal/web/ -run TestDashboardShowsCounts -v` → FAIL (stub has no counts).

- [ ] **Step 3: Implement handler.** In `internal/web/handlers.go` add:
```go
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	counts, _ := s.st.StateCounts(ctx)
	order := []string{"new", "requesting", "requested", "searching", "downloading", "done", "not_found", "failed", "parked", "cancelled", "baseline", "ignored"}
	type cell struct{ Name string; N int }
	var cells []cell
	for _, k := range order {
		if n, ok := counts[k]; ok {
			cells = append(cells, cell{k, n})
		}
	}
	lastRun, _, _ := s.st.GetSetting(ctx, "LAST_RUN")
	cookie, _, _ := s.st.GetSetting(ctx, "GOODREADS_COOKIE")
	feed, _, _ := s.st.GetSetting(ctx, "GOODREADS_FEED_KEY")
	s.render(w, r, "dashboard", "Dashboard", map[string]any{
		"Cells": cells, "LastRun": lastRun,
		"NeedsAuth": cookie == "" && feed == "",
	})
}
```
Remove the `handleDashboard` stub from `handlers_stub.go`.

- [ ] **Step 4: Dashboard template.** Replace `internal/web/templates/dashboard.html`:
```html
{{define "content"}}<h1>Dashboard</h1>
{{if .NeedsAuth}}<div class="flash" style="background:#4a3d2d;border-color:#a83">No Goodreads cookie/feed key set — add one in <a href="/settings">Settings</a>.</div>{{end}}
<div class="cards">{{range .Cells}}<div class="card"><div class="n">{{.N}}</div><div class="muted">{{.Name}}</div></div>{{else}}<p class="muted">No books yet — run a sync.</p>{{end}}</div>
<h2>Actions</h2>
<form method="post" action="/actions/sync" class="inline"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="mode" value="apply"><button>Sync now</button></form>
<form method="post" action="/actions/sync" class="inline"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="mode" value="dryrun"><button style="background:#555">Dry run</button></form>
{{if .LastRun}}<h2>Last run</h2><pre class="muted">{{.LastRun}}</pre>{{end}}{{end}}
```

> CSRF note: the local-bypass path has no session, so `.CSRF` is empty and `requireCSRF` would block local POSTs. In `handleSync` (Task 8), skip the CSRF check when the request is local AND there is no session (mirrors the auth bypass). Document this in Task 8.

- [ ] **Step 5: Run + commit.** `go test ./internal/web/ -v` then `go test ./...`.
```bash
git add internal/web/
git commit -m "feat(web): dashboard with state counts, last-run, auth banner, action buttons"
```

### Task 7: Settings page

**Files:** Modify `internal/web/handlers.go` (add `handleSettings`, remove stub); Modify `internal/web/templates/settings.html`; Create `internal/web/settings_test.go`

**Interfaces:** GET `/settings` renders a form pre-filled from effective config (secrets shown as set/not-set, never echoed); POST saves non-empty fields to store via `SetSetting` (empty secret fields are left unchanged). CSRF enforced (except local-no-session).

- [ ] **Step 1: Failing test** — Create `internal/web/settings_test.go`:
```go
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
		"SHELFARR_URL": {"http://192.168.1.89:5056"},
		"SHELVES":      {"to-read,sci-fi"},
		"SCHEDULE":     {"0 */2 * * *"},
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
```

- [ ] **Step 2: Run — must fail.** `go test ./internal/web/ -run TestSettingsSavePersists -v` → FAIL.

- [ ] **Step 3: Implement handler.** In `internal/web/handlers.go` add:
```go
// settingFields are the non-secret settings editable in the GUI (env-var keys).
var settingFields = []struct{ Key, Label, Kind string }{
	{"SHELFARR_URL", "Shelfarr URL", "text"},
	{"GOODREADS_USER_ID", "Goodreads user id", "text"},
	{"GOODREADS_VISIBILITY", "Goodreads visibility (public/private)", "text"},
	{"SHELVES", "Shelves (comma-separated)", "text"},
	{"FORMAT", "Format (ebook/audiobook)", "text"},
	{"SCHEDULE", "Schedule (cron)", "text"},
	{"MAX_REQUESTS_PER_RUN", "Max requests per run", "text"},
	{"SIMILARITY_THRESHOLD", "Similarity threshold (0-1)", "text"},
	{"FIRST_RUN", "First run (baseline/backfill)", "text"},
	{"LANG_INFERENCE", "Language inference (on/off)", "text"},
	{"SHELFARR_INSECURE", "Allow http to non-loopback Shelfarr (true/false)", "text"},
	{"GUI_PORT", "GUI port", "text"},
	{"AUTH_METHOD", "Auth method (forms/none)", "text"},
	{"AUTH_REQUIRED", "Auth required (enabled/local)", "text"},
}

var secretFields = []struct{ Key, Label string }{
	{"SHELFARR_TOKEN", "Shelfarr API token"},
	{"GOODREADS_COOKIE", "Goodreads session cookie"},
	{"GOODREADS_FEED_KEY", "Goodreads RSS feed key"},
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	if r.Method == http.MethodPost {
		if !s.localNoSession(r) && !s.requireCSRF(w, r) {
			return
		}
		r.ParseForm()
		for _, f := range settingFields {
			if v := r.PostFormValue(f.Key); v != "" {
				s.st.SetSetting(ctx, f.Key, v)
			}
		}
		for _, f := range secretFields {
			if v := r.PostFormValue(f.Key); v != "" { // only overwrite when provided
				s.st.SetSetting(ctx, f.Key, v)
			}
		}
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}
	all, _ := s.st.AllSettings(ctx)
	cfg := s.cfg()
	type field struct{ Key, Label, Value string }
	var fields []field
	cur := map[string]string{
		"SHELFARR_URL": cfg.ShelfarrURL, "GOODREADS_USER_ID": cfg.GoodreadsUserID,
		"GOODREADS_VISIBILITY": all["GOODREADS_VISIBILITY"], "SHELVES": strings.Join(cfg.Shelves, ","),
		"FORMAT": cfg.Format, "SCHEDULE": cfg.Schedule, "MAX_REQUESTS_PER_RUN": itoa(cfg.MaxRequestsPerRun),
		"SIMILARITY_THRESHOLD": ftoa(cfg.SimilarityThreshold), "FIRST_RUN": cfg.FirstRun,
		"LANG_INFERENCE": onoff(cfg.LangInference), "SHELFARR_INSECURE": btoa(cfg.ShelfarrInsecure),
		"GUI_PORT": cfg.GUIPort, "AUTH_METHOD": cfg.AuthMethod, "AUTH_REQUIRED": cfg.AuthRequired,
	}
	for _, f := range settingFields {
		fields = append(fields, field{f.Key, f.Label, cur[f.Key]})
	}
	type secret struct{ Key, Label string; Set bool }
	var secrets []secret
	for _, f := range secretFields {
		_, ok, _ := s.st.GetSetting(ctx, f.Key)
		secrets = append(secrets, secret{f.Key, f.Label, ok})
	}
	s.render(w, r, "settings", "Settings", map[string]any{"Fields": fields, "Secrets": secrets})
}

func (s *Server) localNoSession(r *http.Request) bool {
	return s.session(r) == nil && auth.IsLocalAddr(r.RemoteAddr)
}
```
Add the small formatting helpers to `internal/web/handlers.go`:
```go
import "strconv"

func itoa(n int) string         { return strconv.Itoa(n) }
func ftoa(f float64) string     { return strconv.FormatFloat(f, 'g', -1, 64) }
func btoa(b bool) string        { if b { return "true" }; return "false" }
func onoff(b bool) string       { if b { return "on" }; return "off" }
```
(merge the `strconv` and earlier `strings` imports into the file's import block). Remove the `handleSettings` stub from `handlers_stub.go`. Add `"strings"` import where used.

- [ ] **Step 4: Settings template.** Replace `internal/web/templates/settings.html`:
```html
{{define "content"}}<h1>Settings</h1>
<form method="post" action="/settings"><input type="hidden" name="csrf" value="{{.CSRF}}">
{{range .Fields}}<label>{{.Label}}</label><input name="{{.Key}}" value="{{.Value}}">{{end}}
<h2>Secrets <span class="muted">(leave blank to keep current)</span></h2>
{{range .Secrets}}<label>{{.Label}} — {{if .Set}}<span class="set">set</span>{{else}}<span class="unset">not set</span>{{end}}</label><input name="{{.Key}}" type="password" placeholder="••••••••">{{end}}
<button type="submit">Save</button></form>{{end}}
```

- [ ] **Step 5: Run + commit.** `go test ./internal/web/ -v` then `go test ./...`.
```bash
git add internal/web/
git commit -m "feat(web): settings page — edit all params, write-only secrets, persisted to store"
```

### Task 8: Sync / Dry-run actions

**Files:** Modify `internal/web/handlers.go` (add `handleSync`, remove stub); Create `internal/web/actions_test.go`

**Interfaces:** POST `/actions/sync` with `mode=apply|dryrun` runs `s.run(dryRun)` synchronously, stores the report string in setting `LAST_RUN`, redirects to `/`. CSRF enforced except local-no-session.

- [ ] **Step 1: Failing test** — Create `internal/web/actions_test.go`:
```go
package web

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/engine"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/store"
)

func TestSyncActionRunsAndRecordsLastRun(t *testing.T) {
	st, _ := store.Open(t.TempDir() + "/bb.db")
	defer st.Close()
	called := false
	s := New(st, func(dryRun bool) (engine.Report, error) { called = true; return engine.Report{New: 3, Requested: 2}, nil })
	form := url.Values{"mode": {"apply"}}
	req := httptest.NewRequest("POST", "/actions/sync", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "127.0.0.1:1"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if !called {
		t.Fatal("runner was not invoked")
	}
	if v, _, _ := st.GetSetting(reqCtx(), "LAST_RUN"); !strings.Contains(v, "requested=2") {
		t.Fatalf("LAST_RUN not recorded: %q", v)
	}
}
```

- [ ] **Step 2: Run — must fail.** `go test ./internal/web/ -run TestSyncAction -v` → FAIL.

- [ ] **Step 3: Implement.** In `internal/web/handlers.go` add:
```go
import "fmt"

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if !s.localNoSession(r) && !s.requireCSRF(w, r) {
		return
	}
	r.ParseForm()
	dryRun := r.PostFormValue("mode") == "dryrun"
	rep, err := s.run(dryRun)
	mode := "apply"
	if dryRun {
		mode = "dry-run"
	}
	var summary string
	if err != nil {
		summary = fmt.Sprintf("[%s] error: %v", mode, err)
	} else {
		summary = fmt.Sprintf("[%s] fetched=%d new=%d requested=%d not_found=%d already_exists=%d errors=%d reconciled=%d completed=%d failed=%d rechecked=%d parked=%d",
			mode, rep.Fetched, rep.New, rep.Requested, rep.NotFound, rep.AlreadyExists, rep.Errors,
			rep.Reconciled, rep.Completed, rep.Failed, rep.Rechecked, rep.Parked)
	}
	s.st.SetSetting(context.Background(), "LAST_RUN", summary)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
```
Remove the `handleSync` stub from `handlers_stub.go` (the stub file should now be empty — delete it).

- [ ] **Step 4: Run + commit.** `go test ./internal/web/ -v` then `go test ./...`.
```bash
git add internal/web/
git commit -m "feat(web): Sync now / Dry run actions recording LAST_RUN"
```

### Task 9: Serve the GUI (`web` cmd + daemon) on effective config

**Files:** Modify `cmd/bookbridge/main.go`; Create `cmd/bookbridge/web_test.go`

**Interfaces:** new `bookbridge web` subcommand serves the GUI; `daemon` also serves it concurrently with the scheduler. `buildEngine` reads **effective** config (env+store) and is reused by the web Runner against the SHARED store. Bootstrap admin creds from env `AUTH_USERNAME`/`AUTH_PASSWORD` on startup.

- [ ] **Step 1: Failing test** — Create `cmd/bookbridge/web_test.go`:
```go
package main

import "testing"

func TestRunWebUnknownStillDispatches(t *testing.T) {
	// `web` must be a recognized subcommand (returns !=2 usage code with config error path)
	var out stringSink
	code := run([]string{"web"}, func(k string) string {
		return map[string]string{"SHELFARR_URL": "", "SHELFARR_TOKEN": ""}[k]
	}, &out)
	if code == 2 {
		t.Fatalf("`web` should be dispatched, not usage error; out=%s", out.String())
	}
}

type stringSink struct{ b []byte }

func (s *stringSink) Write(p []byte) (int, error) { s.b = append(s.b, p...); return len(p), nil }
func (s *stringSink) String() string              { return string(s.b) }
```

- [ ] **Step 2: Run — must fail.** `go test ./cmd/bookbridge/ -run TestRunWebUnknown -v` → FAIL.

- [ ] **Step 3: Implement.** In `cmd/bookbridge/main.go`:
  - add imports `"net"` and `"github.com/JanitorHead/shelfarr-bookbridge/internal/auth"`, `"github.com/JanitorHead/shelfarr-bookbridge/internal/web"`.
  - add a `web` case to the `run` switch:
```go
	case "web":
		return runWeb(args[1:], getenv, out)
```
  - refactor `buildEngine` to use effective config + bootstrap admin; add helpers:
```go
// effectiveConfig merges env with stored settings.
func effectiveConfig(st *store.Store, getenv func(string) string) (config.Config, error) {
	all, err := st.AllSettings(context.Background())
	if err != nil {
		return config.Config{}, err
	}
	return config.LoadEffective(getenv, all)
}

// bootstrapAdmin seeds AUTH_USERNAME/AUTH_PASSWORD_HASH from env on first start.
func bootstrapAdmin(st *store.Store, getenv func(string) string) {
	ctx := context.Background()
	if _, ok, _ := st.GetSetting(ctx, "AUTH_PASSWORD_HASH"); ok {
		return
	}
	if pw := getenv("AUTH_PASSWORD"); pw != "" {
		u := getenv("AUTH_USERNAME")
		if u == "" {
			u = "admin"
		}
		if h, err := auth.Hash(pw); err == nil {
			st.SetSetting(ctx, "AUTH_USERNAME", u)
			st.SetSetting(ctx, "AUTH_PASSWORD_HASH", h)
		}
	}
}

// engineFor builds an engine from effective config against an EXISTING store.
func engineFor(cfg config.Config, st *store.Store, getenv func(string) string) (*engine.Engine, error) {
	if err := config.CheckTransport(cfg.ShelfarrURL, cfg.ShelfarrInsecure); err != nil {
		return nil, err
	}
	src := goodreads.NewSource(cfg.GoodreadsUserID, cfg.GoodreadsFeedKey, cfg.GoodreadsCookie, getenv("GOODREADS_BASE"), nil)
	sh := shelfarr.New(cfg.ShelfarrURL, cfg.ShelfarrToken, &http.Client{Timeout: 20 * time.Second})
	e := engine.New(src, st, sh, cfg)
	if cfg.LangInference {
		e.SetDetector(langdetect.New())
	}
	return e, nil
}

func runWeb(args []string, getenv func(string) string, out io.Writer) int {
	st, err := store.Open(orEnv(getenv, "BB_DB", "/config/bookbridge.db"))
	if err != nil {
		fmt.Fprintln(out, "store error:", err)
		return 1
	}
	defer st.Close()
	bootstrapAdmin(st, getenv)
	runner := func(dryRun bool) (engine.Report, error) {
		cfg, err := effectiveConfig(st, getenv)
		if err != nil {
			return engine.Report{}, err
		}
		e, err := engineFor(cfg, st, getenv)
		if err != nil {
			return engine.Report{}, err
		}
		return e.Run(context.Background(), dryRun)
	}
	srv := web.New(st, runner)
	cfg, _ := effectiveConfig(st, getenv)
	addr := net.JoinHostPort(cfg.GUIBind, cfg.GUIPort)
	fmt.Fprintf(out, "BookBridge GUI on http://%s\n", addr)
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		fmt.Fprintln(out, "web error:", err)
		return 1
	}
	return 0
}
```
  - In `runDaemon`, after building the engine, ALSO start the GUI in a goroutine so one container serves both:
```go
	// serve the GUI alongside the scheduler
	go func() {
		runner := func(dryRun bool) (engine.Report, error) {
			cfg, err := effectiveConfig(st, getenv)
			if err != nil {
				return engine.Report{}, err
			}
			e2, err := engineFor(cfg, st, getenv)
			if err != nil {
				return engine.Report{}, err
			}
			return e2.Run(context.Background(), dryRun)
		}
		srv := web.New(st, runner)
		addr := net.JoinHostPort(cfg.GUIBind, cfg.GUIPort)
		fmt.Fprintf(out, "BookBridge GUI on http://%s\n", addr)
		http.ListenAndServe(addr, srv.Handler())
	}()
	bootstrapAdmin(st, getenv)
```
  (Place `bootstrapAdmin` + the goroutine right after `buildEngine` succeeds and before `cycle()`.) Keep the existing `buildEngine` for the daemon's own cycle, but it should also read effective config — update `buildEngine` to call `effectiveConfig` instead of `config.Load2`. Concretely, change `runDaemon` to build cfg via `effectiveConfig(st, getenv)`. Since `buildEngine` opens its own store, refactor `runDaemon` and `runSync` to open the store once and use `engineFor`. (See Step 3b.)

- [ ] **Step 3b: Unify store/engine construction.** Replace `buildEngine` usages so the store is opened once per command and `engineFor` builds the engine from effective config. In `runSync` and `runDaemon`, open the store via `store.Open(orEnv(getenv,"BB_DB","/config/bookbridge.db"))`, call `bootstrapAdmin`, get `cfg, err := effectiveConfig(st, getenv)`, then `e, err := engineFor(cfg, st, getenv)`. Delete the old `buildEngine`. Keep behavior identical for the existing sync/daemon tests (they pass `BB_DB` + envs; effective config with an empty store equals the env-only config).

- [ ] **Step 4: Run — full suite.** `go test ./...` → green (existing `cmd` tests must still pass; effective config with no stored settings == env config).

- [ ] **Step 5: Commit.**
```bash
git add cmd/bookbridge/main.go cmd/bookbridge/web_test.go
git commit -m "feat(cli): web subcommand + GUI served by daemon on effective (env+store) config"
```

### Task 10: Docker / compose / Unraid expose the GUI

**Files:** Modify `Dockerfile`, `docker-compose.yml`, `unraid-template.xml`, `README.md`

- [ ] **Step 1: Dockerfile** — add before the ENTRYPOINT line:
```dockerfile
EXPOSE 7373
```

- [ ] **Step 2: compose** — under the `bookbridge` service add:
```yaml
    ports:
      - "7373:7373"
    environment:
      AUTH_METHOD: "forms"
      AUTH_REQUIRED: "local"
      AUTH_USERNAME: "admin"
      AUTH_PASSWORD: "change-me-on-first-start"
      GUI_PORT: "7373"
```
(merge these into the existing `environment:` block rather than duplicating it.)

- [ ] **Step 3: Unraid template** — add a WebUI attribute to `<Container>` and a port + auth config entries:
```xml
  <WebUI>http://[IP]:[PORT:7373]/</WebUI>
```
and inside the `<Container>`:
```xml
  <Config Name="WebUI Port" Target="7373" Default="7373" Mode="tcp" Description="GUI port" Type="Port" Display="always" Required="true"/>
  <Config Name="Auth Required" Target="AUTH_REQUIRED" Default="local" Mode="" Description="enabled or local (no login from LAN)" Type="Variable" Display="always" Required="false"/>
  <Config Name="Admin User" Target="AUTH_USERNAME" Default="admin" Mode="" Description="GUI admin username" Type="Variable" Display="always" Required="false"/>
  <Config Name="Admin Password" Target="AUTH_PASSWORD" Default="" Mode="" Description="Set on first start (then managed in the GUI)" Type="Variable" Display="always" Required="false" Mask="true"/>
```

- [ ] **Step 4: README** — add a "Web GUI" section:
```markdown
## Web GUI

Browse to `http://<host>:7373`. Auth mirrors *arr: set `AUTH_REQUIRED=local` to skip
login from your LAN (private/loopback addresses) and require it from outside, or
`enabled` to always require it; `AUTH_METHOD=none` disables it entirely. The admin
user/password are seeded from `AUTH_USERNAME`/`AUTH_PASSWORD` on first start, then
managed in Settings. All parameters are editable in the GUI and persist in `/config`
(overriding env). The container runs the daemon and the GUI together.
```

- [ ] **Step 5: Build + smoke.** `export PATH="/c/Program Files/Go/bin:$PATH" && cd /c/source/repo/shelfarr-bookbridge && go build ./... && docker build -t bookbridge:test .`
Expected: builds. Then `go test ./...` green.

- [ ] **Step 6: Commit.**
```bash
git add Dockerfile docker-compose.yml unraid-template.xml README.md
git commit -m "feat: expose web GUI (port 7373) in Docker/compose/Unraid + README"
```

---

## Self-Review (performed)

- **Coverage:** settings persistence + StateCounts (T1); GUI/auth config + LoadEffective store-over-env (T2); argon2id + local-IP auth (T3); server skeleton with *arr-style auth middleware, sessions, security headers, embedded templates/CSS (T4); forms login/logout + first-run admin + CSRF (T5); dashboard with counts/last-run/banner/action buttons (T6); settings page editing all params with write-only secrets (T7); Sync/Dry-run actions (T8); `web` subcommand + GUI served by the daemon on effective config (T9); Docker/Unraid expose port 7373 (T10). Delivers the requested "edit parameters + control, full Docker/Unraid package".
- **Placeholders:** the Task-4 `handlers_stub.go` is an explicit, scaffolded compile shim that Tasks 5–8 remove file-by-file (last removal deletes it) — not a left-behind stub.
- **Type consistency:** `store.GetSetting/SetSetting/AllSettings/StateCounts`, `config.LoadEffective`, `auth.Hash/Verify/IsLocalAddr`, `web.New/Runner/Server.Handler`, `engineFor/effectiveConfig/bootstrapAdmin` are referenced identically across tasks. Setting keys are env-var names throughout.

## Phase 3b (next plan): Shelves page (per-shelf enable/format/language), Queue/Requested table with filters, Not-found Review (retry/ignore/manual work_id).
