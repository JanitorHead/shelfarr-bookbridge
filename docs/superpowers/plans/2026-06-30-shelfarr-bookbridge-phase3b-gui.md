# Shelfarr BookBridge — Phase 3b: GUI Management Pages (queue, review, shelves)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Complete the GUI with a Queue/Requested table, a Not-found Review page (retry/ignore), and a Shelves page (per-shelf enable + format) — with the engine honoring enabled shelves and per-shelf format.

**Architecture:** New read/action store queries back three new pages registered on the existing web server. The engine fetches only enabled shelves (`store.EnabledShelves`) and chooses each book's request format from the highest-priority shelf override (`store.ShelfFormat`), falling back to the global `FORMAT`.

**Tech Stack:** existing (Go stdlib net/http + html/template).

## Global Constraints

- Module `github.com/JanitorHead/shelfarr-bookbridge`; TDD; `go test ./...` green per task; commit per task with `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
- Setting/shelf keys unchanged. `shelf_config(shelf PK, enabled, baselined_at, format, language)` already exists (schema v1).
- GUI POST actions require CSRF except local-no-session (reuse `localNoSession`/`requireCSRF`).

---

## File Structure

| Path | Responsibility | Change |
|---|---|---|
| `internal/store/books.go` | `ListBooks`, `IgnoreBook`, `RetryBook` | Modify |
| `internal/store/shelfconfig.go` | shelf_config accessors + EnabledShelves/ShelfFormat | Create |
| `internal/engine/engine.go` | fetch enabled shelves + per-shelf format | Modify |
| `internal/web/server.go` | register /queue /review /shelves | Modify |
| `internal/web/pages.go` | queue/review/shelves handlers | Create |
| `internal/web/templates/{queue,review,shelves}.html` | pages | Create |

---

### Task 1: Store — list books, book actions, shelf config

**Files:** Modify `internal/store/books.go`; Create `internal/store/shelfconfig.go`, `internal/store/manage_test.go`

**Interfaces:**
- `BookRow{Source,ExternalID,Title,Author,State,WorkID,RequestID,Language string; AttemptCount int}`
- `(*Store).ListBooks(ctx, state string, limit int) ([]BookRow, error)` (state "" = all)
- `(*Store).IgnoreBook(ctx, source, externalID) error`; `RetryBook(ctx, source, externalID) error`
- `ShelfCfg{Shelf string; Enabled bool; Format, Language string}`
- `(*Store).ShelfConfigs(ctx, configured []string) ([]ShelfCfg, error)`; `SetShelfConfig(ctx, shelf string, enabled bool, format, language string) error`
- `(*Store).EnabledShelves(ctx, configured []string) ([]string, error)`; `ShelfFormat(ctx, shelf string) (string, bool)`

- [ ] **Step 1: Failing test** — Create `internal/store/manage_test.go`:
```go
package store

import (
	"context"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

func TestListBooksAndActions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	s.Diff(ctx, []sources.Book{
		{Source: "goodreads", ExternalID: "1", Title: "A", Author: "X"},
		{Source: "goodreads", ExternalID: "2", Title: "B", Author: "Y"},
	})
	s.SetState(ctx, sources.Book{Source: "goodreads", ExternalID: "2"}, "not_found")
	if rows, _ := s.ListBooks(ctx, "not_found", 50); len(rows) != 1 || rows[0].ExternalID != "2" {
		t.Fatalf("ListBooks filter: %+v", rows)
	}
	if all, _ := s.ListBooks(ctx, "", 50); len(all) != 2 {
		t.Fatalf("ListBooks all: %d", len(all))
	}
	if err := s.IgnoreBook(ctx, "goodreads", "1"); err != nil {
		t.Fatal(err)
	}
	if rows, _ := s.ListBooks(ctx, "ignored", 50); len(rows) != 1 {
		t.Fatal("ignore failed")
	}
	if err := s.RetryBook(ctx, "goodreads", "2"); err != nil {
		t.Fatal(err)
	}
	if rows, _ := s.ListBooks(ctx, "new", 50); len(rows) != 1 || rows[0].ExternalID != "2" {
		t.Fatalf("retry should reset to new: %+v", rows)
	}
}

func TestShelfConfig(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	configured := []string{"to-read", "sci-fi"}
	if err := s.SetShelfConfig(ctx, "sci-fi", false, "audiobook", ""); err != nil {
		t.Fatal(err)
	}
	cfgs, _ := s.ShelfConfigs(ctx, configured)
	if len(cfgs) != 2 {
		t.Fatalf("want 2 shelf cfgs, got %d", len(cfgs))
	}
	en, _ := s.EnabledShelves(ctx, configured)
	if len(en) != 1 || en[0] != "to-read" {
		t.Fatalf("EnabledShelves should drop disabled sci-fi: %v", en)
	}
	if f, ok := s.ShelfFormat(ctx, "sci-fi"); !ok || f != "audiobook" {
		t.Fatalf("ShelfFormat: %q %v", f, ok)
	}
}
```

- [ ] **Step 2: Run — must fail.** `export PATH="/c/Program Files/Go/bin:$PATH" && cd /c/source/repo/shelfarr-bookbridge && go test ./internal/store/ -run "TestListBooks|TestShelfConfig" -v` → FAIL.

- [ ] **Step 3: Book list + actions.** Append to `internal/store/books.go`:
```go
type BookRow struct {
	Source, ExternalID, Title, Author, State, WorkID, RequestID, Language string
	AttemptCount                                                          int
}

func (s *Store) ListBooks(ctx context.Context, state string, limit int) ([]BookRow, error) {
	if limit <= 0 {
		limit = 500
	}
	q := `SELECT source,external_id,title,author,state,COALESCE(work_id,''),COALESCE(shelfarr_request_id,''),COALESCE(chosen_language,''),attempt_count FROM books`
	args := []any{}
	if state != "" {
		q += ` WHERE state=?`
		args = append(args, state)
	}
	q += ` ORDER BY updated_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BookRow
	for rows.Next() {
		var b BookRow
		if err := rows.Scan(&b.Source, &b.ExternalID, &b.Title, &b.Author, &b.State, &b.WorkID, &b.RequestID, &b.Language, &b.AttemptCount); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) IgnoreBook(ctx context.Context, source, externalID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE books SET state='ignored', updated_at=datetime('now') WHERE source=? AND external_id=?`, source, externalID)
	return err
}

func (s *Store) RetryBook(ctx context.Context, source, externalID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE books SET state='new', attempt_count=0, shelfarr_request_id='', updated_at=datetime('now') WHERE source=? AND external_id=?`, source, externalID)
	return err
}
```

- [ ] **Step 4: Shelf config.** Create `internal/store/shelfconfig.go`:
```go
package store

import "context"

type ShelfCfg struct {
	Shelf            string
	Enabled          bool
	Format, Language string
}

func (s *Store) SetShelfConfig(ctx context.Context, shelf string, enabled bool, format, language string) error {
	en := 0
	if enabled {
		en = 1
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO shelf_config(shelf,enabled,format,language) VALUES(?,?,?,?)
		 ON CONFLICT(shelf) DO UPDATE SET enabled=excluded.enabled, format=excluded.format, language=excluded.language`,
		shelf, en, format, language)
	return err
}

// ShelfConfigs returns config for each configured shelf (defaults: enabled, no overrides).
func (s *Store) ShelfConfigs(ctx context.Context, configured []string) ([]ShelfCfg, error) {
	out := make([]ShelfCfg, 0, len(configured))
	for _, sh := range configured {
		c := ShelfCfg{Shelf: sh, Enabled: true}
		var en int
		var f, l *string
		err := s.db.QueryRowContext(ctx, `SELECT enabled, format, language FROM shelf_config WHERE shelf=?`, sh).Scan(&en, &f, &l)
		if err == nil {
			c.Enabled = en != 0
			if f != nil {
				c.Format = *f
			}
			if l != nil {
				c.Language = *l
			}
		}
		out = append(out, c)
	}
	return out, nil
}

func (s *Store) EnabledShelves(ctx context.Context, configured []string) ([]string, error) {
	cfgs, err := s.ShelfConfigs(ctx, configured)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, c := range cfgs {
		if c.Enabled {
			out = append(out, c.Shelf)
		}
	}
	return out, nil
}

func (s *Store) ShelfFormat(ctx context.Context, shelf string) (string, bool) {
	var f *string
	if err := s.db.QueryRowContext(ctx, `SELECT format FROM shelf_config WHERE shelf=?`, shelf).Scan(&f); err != nil {
		return "", false
	}
	if f == nil || *f == "" {
		return "", false
	}
	return *f, true
}
```

- [ ] **Step 5: Run + commit.** `go test ./internal/store/ -v` then `go test ./...`.
```bash
git add internal/store/books.go internal/store/shelfconfig.go internal/store/manage_test.go
git commit -m "feat(store): list books, ignore/retry actions, shelf_config accessors"
```

### Task 2: Engine — enabled shelves + per-shelf format

**Files:** Modify `internal/engine/engine.go`; Create `internal/engine/pershelf_test.go`

**Interfaces:** `Run` fetches `store.EnabledShelves(cfg.Shelves)`; requests use `e.formatFor(ctx,b)` (per-shelf override by configured priority, else `cfg.Format`).

- [ ] **Step 1: Failing test** — Create `internal/engine/pershelf_test.go`:
```go
package engine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/shelfarr"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/store"
)

func TestEngineUsesPerShelfFormat(t *testing.T) {
	var gotType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/search":
			w.Write([]byte(`{"results":[{"work_id":"ol:1","title":"Dune","author":"Frank Herbert","confidence":90}]}`))
		case strings.HasPrefix(r.URL.Path, "/api/v1/requests/"):
			w.Write([]byte(`{"status":"downloading"}`))
		case r.URL.Path == "/api/v1/requests":
			body, _ := io.ReadAll(r.Body)
			var m map[string]any
			json.Unmarshal(body, &m)
			if bt, _ := m["book_types"].([]any); len(bt) > 0 {
				gotType, _ = bt[0].(string)
			}
			w.WriteHeader(201)
			w.Write([]byte(`{"requests":[{"id":1}],"errors":[]}`))
		}
	}))
	defer srv.Close()
	st, _ := store.Open(t.TempDir() + "/bb.db")
	defer st.Close()
	ctx := context.Background()
	st.SetShelfConfig(ctx, "audio", true, "audiobook", "")
	sh := shelfarr.New(srv.URL, config.SecretString("t"), nil)
	src := fixedSource{[]sources.Book{{Source: "goodreads", ExternalID: "1", Title: "Dune", Author: "Frank Herbert", Shelves: []string{"audio"}}}}
	e := New(src, st, sh, config.Config{Format: "ebook", SimilarityThreshold: 0.82, MaxRequestsPerRun: 25, Shelves: []string{"audio"}})
	if _, err := e.Run(ctx, false); err != nil {
		t.Fatal(err)
	}
	if gotType != "audiobook" {
		t.Fatalf("per-shelf format override not applied, got %q", gotType)
	}
}
```

- [ ] **Step 2: Run — must fail.** `go test ./internal/engine/ -run TestEngineUsesPerShelfFormat -v` → FAIL.

- [ ] **Step 3: Implement.** In `internal/engine/engine.go`:
  - In `Run`, replace `books, err := e.src.Fetch(ctx, e.cfg.Shelves)` with:
```go
	shelves, err := e.st.EnabledShelves(ctx, e.cfg.Shelves)
	if err != nil {
		return rep, err
	}
	books, err := e.src.Fetch(ctx, shelves)
```
  - Add a helper method:
```go
// formatFor picks the request format from the highest-priority shelf override
// (by configured order) the book belongs to, else the global format.
func (e *Engine) formatFor(ctx context.Context, b sources.Book) string {
	shelves, _ := e.st.ShelvesOf(ctx, b.Source, b.ExternalID)
	in := map[string]bool{}
	for _, sh := range shelves {
		in[sh] = true
	}
	for _, cs := range e.cfg.Shelves {
		if in[cs] {
			if f, ok := e.st.ShelfFormat(ctx, cs); ok {
				return f
			}
		}
	}
	return e.cfg.Format
}
```
  - In the main request loop, change `BookTypes: []string{e.cfg.Format}` to `BookTypes: []string{e.formatFor(ctx, b)}`.
  - In `resolveAndRequest`, change `BookTypes: []string{e.cfg.Format}` to `BookTypes: []string{e.formatFor(ctx, b)}`.

- [ ] **Step 4: Run + commit.** `go test ./internal/engine/ -v` then `go test ./...` (existing engine tests have no shelf_config rows, so `formatFor` returns the global `Format` — behavior unchanged).
```bash
git add internal/engine/engine.go internal/engine/pershelf_test.go
git commit -m "feat(engine): fetch only enabled shelves + per-shelf format override"
```

### Task 3: Queue + Review pages

**Files:** Modify `internal/web/server.go` (register `/queue`,`/review`); Create `internal/web/pages.go`, `internal/web/templates/queue.html`, `internal/web/templates/review.html`, `internal/web/pages_test.go`

**Interfaces:** GET `/queue?state=` renders a table (`store.ListBooks`); GET `/review` lists not_found+parked; POST `/review` with `action=ignore|retry` + `source` + `external_id` performs the action then redirects.

- [ ] **Step 1: Failing test** — Create `internal/web/pages_test.go`:
```go
package web

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

func TestQueueListsBooks(t *testing.T) {
	s := testServer(t)
	s.st.Diff(reqCtx(), []sources.Book{{Source: "goodreads", ExternalID: "1", Title: "Dune", Author: "Frank Herbert"}})
	req := httptest.NewRequest("GET", "/queue", nil)
	req.RemoteAddr = "127.0.0.1:1"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "Dune") {
		t.Fatalf("queue missing book: %s", rec.Body.String())
	}
}

func TestReviewIgnoreAction(t *testing.T) {
	s := testServer(t)
	ctx := reqCtx()
	s.st.Diff(ctx, []sources.Book{{Source: "goodreads", ExternalID: "1", Title: "Z", Author: "Q"}})
	s.st.SetState(ctx, sources.Book{Source: "goodreads", ExternalID: "1"}, "not_found")
	form := url.Values{"action": {"ignore"}, "source": {"goodreads"}, "external_id": {"1"}}
	req := httptest.NewRequest("POST", "/review", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "127.0.0.1:1"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	rows, _ := s.st.ListBooks(ctx, "ignored", 10)
	if len(rows) != 1 {
		t.Fatalf("ignore action did not apply: %+v", rows)
	}
}
```

- [ ] **Step 2: Run — must fail.** `go test ./internal/web/ -run "TestQueue|TestReview" -v` → FAIL.

- [ ] **Step 3: Register routes.** In `internal/web/server.go`, in `Handler()`, add before the `/` route:
```go
	mux.HandleFunc("/queue", s.guard(s.handleQueue))
	mux.HandleFunc("/review", s.guard(s.handleReview))
	mux.HandleFunc("/shelves", s.guard(s.handleShelves))
```

- [ ] **Step 4: Handlers.** Create `internal/web/pages.go`:
```go
package web

import (
	"context"
	"net/http"
)

func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	state := r.URL.Query().Get("state")
	rows, _ := s.st.ListBooks(ctx, state, 500)
	s.render(w, r, "queue", "Queue", map[string]any{"Rows": rows, "State": state})
}

func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	if r.Method == http.MethodPost {
		if !s.localNoSession(r) && !s.requireCSRF(w, r) {
			return
		}
		r.ParseForm()
		src, id := r.PostFormValue("source"), r.PostFormValue("external_id")
		switch r.PostFormValue("action") {
		case "ignore":
			s.st.IgnoreBook(ctx, src, id)
		case "retry":
			s.st.RetryBook(ctx, src, id)
		}
		http.Redirect(w, r, "/review", http.StatusSeeOther)
		return
	}
	nf, _ := s.st.ListBooks(ctx, "not_found", 500)
	parked, _ := s.st.ListBooks(ctx, "parked", 500)
	s.render(w, r, "review", "Review", map[string]any{"Rows": append(nf, parked...)})
}
```

- [ ] **Step 5: Templates.** Create `internal/web/templates/queue.html`:
```html
{{define "content"}}<h1>Queue</h1>
<p class="muted">Filter: <a href="/queue">all</a> · <a href="/queue?state=downloading">downloading</a> · <a href="/queue?state=searching">searching</a> · <a href="/queue?state=requested">requested</a> · <a href="/queue?state=done">done</a> · <a href="/queue?state=not_found">not_found</a></p>
<table><thead><tr><th>Title</th><th>Author</th><th>State</th><th>Lang</th><th>work_id</th></tr></thead><tbody>
{{range .Rows}}<tr><td>{{.Title}}</td><td>{{.Author}}</td><td>{{.State}}</td><td>{{.Language}}</td><td class="muted">{{.WorkID}}</td></tr>{{else}}<tr><td colspan="5" class="muted">No books.</td></tr>{{end}}
</tbody></table>{{end}}
```
Create `internal/web/templates/review.html`:
```html
{{define "content"}}<h1>Review — not found / parked</h1>
<table><thead><tr><th>Title</th><th>Author</th><th>State</th><th>Att.</th><th>Actions</th></tr></thead><tbody>
{{range .Rows}}<tr><td>{{.Title}}</td><td>{{.Author}}</td><td>{{.State}}</td><td>{{.AttemptCount}}</td>
<td><form method="post" action="/review" class="inline"><input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="source" value="{{.Source}}"><input type="hidden" name="external_id" value="{{.ExternalID}}"><button name="action" value="retry">Retry</button> <button name="action" value="ignore" style="background:#555">Ignore</button></form></td></tr>
{{else}}<tr><td colspan="5" class="muted">Nothing to review 🎉</td></tr>{{end}}
</tbody></table>{{end}}
```

- [ ] **Step 6: Run + commit.** `go test ./internal/web/ -v` then `go test ./...`.
```bash
git add internal/web/server.go internal/web/pages.go internal/web/pages_test.go internal/web/templates/queue.html internal/web/templates/review.html
git commit -m "feat(web): queue table + not-found review (retry/ignore)"
```

### Task 4: Shelves page

**Files:** Modify `internal/web/pages.go` (add `handleShelves`); Create `internal/web/templates/shelves.html`, `internal/web/shelves_test.go`

**Interfaces:** GET `/shelves` lists `store.ShelfConfigs(cfg.Shelves)`; POST saves per-shelf `enabled_<shelf>`/`format_<shelf>`/`language_<shelf>`.

- [ ] **Step 1: Failing test** — Create `internal/web/shelves_test.go`:
```go
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
```

- [ ] **Step 2: Run — must fail.** `go test ./internal/web/ -run TestShelvesSave -v` → FAIL.

- [ ] **Step 3: Implement.** In `internal/web/pages.go` add:
```go
func (s *Server) handleShelves(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	cfg := s.cfg()
	if r.Method == http.MethodPost {
		if !s.localNoSession(r) && !s.requireCSRF(w, r) {
			return
		}
		r.ParseForm()
		// A POST carries one shelf row (field "shelf"); save just that row.
		sh := r.PostFormValue("shelf")
		if sh != "" {
			enabled := r.PostFormValue("enabled_"+sh) != ""
			s.st.SetShelfConfig(ctx, sh, enabled, r.PostFormValue("format_"+sh), r.PostFormValue("language_"+sh))
		}
		http.Redirect(w, r, "/shelves", http.StatusSeeOther)
		return
	}
	cfgs, _ := s.st.ShelfConfigs(ctx, cfg.Shelves)
	s.render(w, r, "shelves", "Shelves", map[string]any{"Shelves": cfgs})
}
```

- [ ] **Step 4: Template.** Create `internal/web/templates/shelves.html`:
```html
{{define "content"}}<h1>Shelves</h1>
<p class="muted">Configured shelves come from <a href="/settings">Settings → Shelves</a>. Per-shelf format overrides the global format; blank = use global.</p>
{{range .Shelves}}
<form method="post" action="/shelves" style="border:1px solid #333;border-radius:8px;padding:.8rem 1rem;margin-bottom:.8rem">
<input type="hidden" name="csrf" value="{{$.CSRF}}"><input type="hidden" name="shelf" value="{{.Shelf}}">
<strong>{{.Shelf}}</strong>
<label><input type="checkbox" name="enabled_{{.Shelf}}" value="1" {{if .Enabled}}checked{{end}} style="width:auto"> Enabled</label>
<label>Format override (blank/ebook/audiobook)</label><input name="format_{{.Shelf}}" value="{{.Format}}">
<label>Language override (blank/es/en)</label><input name="language_{{.Shelf}}" value="{{.Language}}">
<button type="submit">Save shelf</button></form>
{{else}}<p class="muted">No shelves configured.</p>{{end}}{{end}}
```

- [ ] **Step 5: Run + commit.** `go test ./internal/web/ -v` then `go test ./...`.
```bash
git add internal/web/pages.go internal/web/templates/shelves.html internal/web/shelves_test.go
git commit -m "feat(web): shelves page (per-shelf enable + format/language override)"
```

---

## Self-Review (performed)

- **Coverage:** store list/ignore/retry + shelf_config accessors (T1); engine honors enabled shelves + per-shelf format (T2); Queue table with state filter + Review retry/ignore actions (T3); Shelves per-shelf enable/format/language page (T4). With Phase 3a, the GUI is now "complete": dashboard, settings, shelves, queue, review, actions.
- **Placeholders:** none.
- **Type consistency:** `BookRow`, `ListBooks`, `IgnoreBook`, `RetryBook`, `ShelfCfg`, `ShelfConfigs`, `SetShelfConfig`, `EnabledShelves`, `ShelfFormat`, `formatFor`, and handlers `handleQueue/handleReview/handleShelves` (registered in `Handler()`) are referenced consistently. The base template's existing nav links (/shelves /queue /review) now resolve.
```
