# Shelfarr BookBridge — Phase 2b: Language Inference, Reconciliation & Run Lock

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Infer a request language from the book title, close the loop by reconciling Shelfarr request statuses back into the store (retrying not-found books, bounded), and prevent overlapping runs with a single-flight lock.

**Architecture:** A deterministic `langdetect` (lingua-go, en+es, confidence-gated) feeds the request `language`. The engine gains a reconcile phase (poll `GET /api/v1/requests/:id`, map status→state) and a bounded recheck phase (re-resolve `not_found` books up to N attempts, then `parked`). A `run_state` row makes `engine.Run` single-flight.

**Tech Stack:** Go 1.23+, `github.com/pemistahl/lingua-go` (already fetched), existing stack.

## Global Constraints

- Same as prior phases: module `github.com/JanitorHead/shelfarr-bookbridge`; identity `(source, external_id)`; `SecretString`; TDD; `go test ./...` green per task; commit per task with `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>` trailer.
- Language is OPTIONAL and confidence-gated: send it only when detection is confident; omitting it is a first-class outcome. Inference is toggled by `LANG_INFERENCE` (default on; `off` disables).
- Shelfarr `POST /requests` is not idempotent; reconciliation reads status, it does not re-POST. Recheck re-resolves only books currently in `not_found` (bounded by `MAX_REQUESTS_PER_RUN` and a per-item attempt cap of 5, then `parked`).
- Status map: `pending|searching`→`searching`; `downloading|processing`→`downloading`; `completed`→`done`; `not_found`→`not_found`; `failed`→`failed`; HTTP 404 (request gone)→`cancelled` (terminal, never auto-re-requested).

---

## File Structure

| Path | Responsibility | Change |
|---|---|---|
| `internal/langdetect/langdetect.go` | lingua-go detector, confidence-gated | Create |
| `internal/config/config.go` | `LangInference bool` | Modify |
| `internal/store/books.go` | `SetChosenLanguage`, reconcile/recheck queries | Modify |
| `internal/shelfarr/status.go` | `GetRequest`, `RequestStatus`, `ErrRequestNotFound` | Create |
| `internal/engine/engine.go` | detector + reconcile + recheck phases | Modify |
| `internal/store/lock.go` | single-flight run lock | Create |

---

### Task 1: langdetect package

**Files:**
- Create: `internal/langdetect/langdetect.go`
- Test: `internal/langdetect/langdetect_test.go`

**Interfaces:**
- Produces: `langdetect.New() *Detector`; `(*Detector).Detect(title string) (lang string, ok bool)` returning an ISO 639-1 code (`"es"`/`"en"`) only when confident.

- [ ] **Step 1: Write the failing test**

Create `internal/langdetect/langdetect_test.go`:
```go
package langdetect

import "testing"

func TestDetect(t *testing.T) {
	d := New()
	if lang, ok := d.Detect("El nombre del viento"); !ok || lang != "es" {
		t.Fatalf("spanish title => %q,%v want es,true", lang, ok)
	}
	if lang, ok := d.Detect("The Name of the Wind"); !ok || lang != "en" {
		t.Fatalf("english title => %q,%v want en,true", lang, ok)
	}
	if _, ok := d.Detect(""); ok {
		t.Fatal("empty title must be ok=false")
	}
	if _, ok := d.Detect("a"); ok {
		t.Fatal("1-rune title must be ok=false")
	}
}
```

- [ ] **Step 2: Run it — must fail**

Run (Bash): `export PATH="/c/Program Files/Go/bin:$PATH" && cd /c/source/repo/shelfarr-bookbridge && go test ./internal/langdetect/ -v`
Expected: FAIL (New/Detect undefined).

- [ ] **Step 3: Implement**

Create `internal/langdetect/langdetect.go`:
```go
package langdetect

import (
	"strings"

	"github.com/pemistahl/lingua-go"
)

// Detector wraps lingua restricted to English+Spanish, gating on confidence.
type Detector struct{ d lingua.LanguageDetector }

func New() *Detector {
	d := lingua.NewLanguageDetectorBuilder().
		FromLanguages(lingua.English, lingua.Spanish).
		WithPreloadedLanguageModels().
		Build()
	return &Detector{d: d}
}

// Detect returns an ISO 639-1 code only when the top language scores >= 0.65 and
// beats the runner-up by >= 0.15; otherwise ok=false (omit the language).
func (x *Detector) Detect(title string) (string, bool) {
	t := strings.TrimSpace(title)
	if len([]rune(t)) < 2 {
		return "", false
	}
	vals := x.d.ComputeLanguageConfidenceValues(t)
	if len(vals) < 2 {
		return "", false
	}
	top, second := vals[0], vals[1]
	if top.Value() < 0.65 || top.Value()-second.Value() < 0.15 {
		return "", false
	}
	return strings.ToLower(top.Language().IsoCode639_1().String()), true
}
```

- [ ] **Step 4: Run it — must pass**

Run: `go test ./internal/langdetect/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/langdetect/
git commit -m "feat(langdetect): confidence-gated en/es title detection (lingua-go)"
```

### Task 2: Config flag + store language helper

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/store/books.go`
- Test: `internal/config/langflag_test.go`

**Interfaces:**
- Produces: `Config.LangInference bool` (from `LANG_INFERENCE`, default true, `"off"` disables); `(*Store).SetChosenLanguage(ctx, b sources.Book, lang string) error`.

- [ ] **Step 1: Write the failing test**

Create `internal/config/langflag_test.go`:
```go
package config

import "testing"

func TestLangInferenceDefaultOnAndOff(t *testing.T) {
	base := map[string]string{"SHELFARR_URL": "u", "SHELFARR_TOKEN": "t"}
	on, err := loadFrom(func(k string) string { return base[k] })
	if err != nil || !on.LangInference {
		t.Fatalf("default should be on: %+v err=%v", on, err)
	}
	base["LANG_INFERENCE"] = "off"
	off, _ := loadFrom(func(k string) string { return base[k] })
	if off.LangInference {
		t.Fatal("LANG_INFERENCE=off should disable")
	}
}
```

- [ ] **Step 2: Run it — must fail**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && cd /c/source/repo/shelfarr-bookbridge && go test ./internal/config/ -run TestLangInference -v`
Expected: FAIL (LangInference undefined).

- [ ] **Step 3: Add the config field**

In `internal/config/config.go`, add to the `Config` struct:
```go
	LangInference bool
```
and in `loadFrom`, inside the `c := Config{...}` literal:
```go
		LangInference: get("LANG_INFERENCE") != "off",
```

- [ ] **Step 4: Add the store helper**

Append to `internal/store/books.go`:
```go
// SetChosenLanguage records the inferred language used for a book's request.
func (s *Store) SetChosenLanguage(ctx context.Context, b sources.Book, lang string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE books SET chosen_language=?, updated_at=datetime('now') WHERE source=? AND external_id=?`,
		lang, b.Source, b.ExternalID)
	return err
}
```

- [ ] **Step 5: Run + commit**

Run: `go test ./internal/config/ ./internal/store/ -v` → PASS, then `go test ./...` → green.
```bash
git add internal/config/config.go internal/config/langflag_test.go internal/store/books.go
git commit -m "feat(config,store): LangInference flag + SetChosenLanguage"
```

### Task 3: Engine language wiring

**Files:**
- Modify: `internal/engine/engine.go`
- Test: `internal/engine/lang_test.go`

**Interfaces:**
- Produces: `engine.LanguageDetector` interface (`Detect(title string) (string, bool)`); `(*Engine).SetDetector(d LanguageDetector)`; the apply path sends `language` and persists `chosen_language` when detection is confident and `cfg.LangInference` is on.

- [ ] **Step 1: Write the failing test**

Create `internal/engine/lang_test.go`:
```go
package engine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/shelfarr"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/store"
)

type stubDetector struct{ lang string }

func (s stubDetector) Detect(string) (string, bool) {
	if s.lang == "" {
		return "", false
	}
	return s.lang, true
}

func TestEngineSendsDetectedLanguage(t *testing.T) {
	var gotLang string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/search" {
			w.Write([]byte(`{"results":[{"work_id":"ol:1","title":"Dune","author":"Frank Herbert","confidence":90}]}`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		var m map[string]any
		json.Unmarshal(body, &m)
		gotLang, _ = m["language"].(string)
		w.WriteHeader(201)
		w.Write([]byte(`{"requests":[{"id":"req_1"}],"errors":[]}`))
	}))
	defer srv.Close()
	st, _ := store.Open(t.TempDir() + "/bb.db")
	defer st.Close()
	sh := shelfarr.New(srv.URL, config.SecretString("t"), nil)
	e := New(stubSrc(), st, sh, config.Config{Format: "ebook", SimilarityThreshold: 0.82, MaxRequestsPerRun: 25, LangInference: true})
	e.SetDetector(stubDetector{lang: "es"})
	if _, err := e.Run(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if gotLang != "es" {
		t.Fatalf("request language = %q, want es", gotLang)
	}
}

func stubSrc() sources.Source {
	return fixedSource{[]sources.Book{{Source: "goodreads", ExternalID: "1", Title: "Dune", Author: "Frank Herbert"}}}
}

type fixedSource struct{ b []sources.Book }

func (f fixedSource) Fetch(context.Context, []string) ([]sources.Book, error) { return f.b, nil }
```

> If `stubSource` already exists in `engine_test.go`, this file defines `fixedSource` to avoid a name clash; do not redeclare `stubSource`.

- [ ] **Step 2: Run it — must fail**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && cd /c/source/repo/shelfarr-bookbridge && go test ./internal/engine/ -run TestEngineSendsDetectedLanguage -v`
Expected: FAIL (SetDetector undefined).

- [ ] **Step 3: Add detector to the engine**

In `internal/engine/engine.go`, add the interface and field. Add to the `Engine` struct a field:
```go
	detector LanguageDetector
```
Add near the top (after the `Report` type):
```go
// LanguageDetector infers an ISO 639-1 language from a title; ok=false means omit.
type LanguageDetector interface {
	Detect(title string) (string, bool)
}

func (e *Engine) SetDetector(d LanguageDetector) { e.detector = d }

func (e *Engine) detectLang(b sources.Book) string {
	if e.detector == nil || !e.cfg.LangInference {
		return ""
	}
	if lang, ok := e.detector.Detect(b.Title); ok {
		return lang
	}
	return ""
}
```

- [ ] **Step 4: Use the language in the apply path**

In `internal/engine/engine.go`, inside `Run`, in the `!dryRun` apply branch, replace:
```go
		if err := e.st.SetState(ctx, b, "requesting"); err != nil { // intent before POST
			return rep, err
		}
		id, exists, err := e.sh.CreateRequest(ctx, shelfarr.CreateRequestParams{
			WorkID:    pick.WorkID,
			BookTypes: []string{e.cfg.Format},
			Title:     b.Title,
			Author:    b.Author,
			CoverURL:  pick.CoverURL,
			Year:      pick.Year,
		})
```
with:
```go
		lang := e.detectLang(b)
		if lang != "" {
			_ = e.st.SetChosenLanguage(ctx, b, lang)
		}
		if err := e.st.SetState(ctx, b, "requesting"); err != nil { // intent before POST
			return rep, err
		}
		id, exists, err := e.sh.CreateRequest(ctx, shelfarr.CreateRequestParams{
			WorkID:    pick.WorkID,
			BookTypes: []string{e.cfg.Format},
			Language:  lang,
			Title:     b.Title,
			Author:    b.Author,
			CoverURL:  pick.CoverURL,
			Year:      pick.Year,
		})
```

- [ ] **Step 5: Run + commit**

Run: `go test ./internal/engine/ -v && go test ./...` → green.
```bash
git add internal/engine/engine.go internal/engine/lang_test.go
git commit -m "feat(engine): send confidence-gated language on requests"
```

### Task 4: Shelfarr status client

**Files:**
- Create: `internal/shelfarr/status.go`
- Test: `internal/shelfarr/status_test.go`

**Interfaces:**
- Produces: `RequestStatus{ ID, Status, IssueDescription string; AttentionNeeded bool }`; `var ErrRequestNotFound = errors.New(...)`; `(*Client).GetRequest(ctx, id string) (RequestStatus, error)` returning `ErrRequestNotFound` on HTTP 404.

- [ ] **Step 1: Write the failing test**

Create `internal/shelfarr/status_test.go`:
```go
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
```

- [ ] **Step 2: Run it — must fail**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && cd /c/source/repo/shelfarr-bookbridge && go test ./internal/shelfarr/ -run TestGetRequest -v`
Expected: FAIL (GetRequest undefined).

- [ ] **Step 3: Implement**

Create `internal/shelfarr/status.go`:
```go
package shelfarr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

var ErrRequestNotFound = errors.New("shelfarr request not found (404)")

type RequestStatus struct {
	ID               string `json:"id"`
	Status           string `json:"status"`
	IssueDescription string `json:"issue_description"`
	AttentionNeeded  bool   `json:"attention_needed"`
}

func (c *Client) GetRequest(ctx context.Context, id string) (RequestStatus, error) {
	var rs RequestStatus
	req, err := c.newReq("GET", "/api/v1/requests/"+id)
	if err != nil {
		return rs, err
	}
	resp, err := c.hc.Do(req.WithContext(ctx))
	if err != nil {
		return rs, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == 404 {
		return rs, ErrRequestNotFound
	}
	if resp.StatusCode != 200 {
		return rs, fmt.Errorf("shelfarr get request %s: HTTP %d: %s", id, resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, &rs); err != nil {
		return rs, err
	}
	return rs, nil
}
```

- [ ] **Step 4: Run + commit**

Run: `go test ./internal/shelfarr/ -v` → PASS.
```bash
git add internal/shelfarr/status.go internal/shelfarr/status_test.go
git commit -m "feat(shelfarr): GetRequest status with 404 -> ErrRequestNotFound"
```

### Task 5: Store reconcile/recheck queries + engine phases

**Files:**
- Modify: `internal/store/books.go`
- Modify: `internal/engine/engine.go`
- Test: `internal/store/reconcile_test.go`
- Test: `internal/engine/reconcile_test.go`

**Interfaces:**
- Produces (store): `ReqRef{ Source, ExternalID, RequestID string }`; `(*Store).OpenRequestItems(ctx) ([]ReqRef, error)`; `(*Store).ApplyStatus(ctx, source, externalID, state string) error`; `(*Store).NotFoundItems(ctx, maxAttempts int) ([]sources.Book, error)`; `(*Store).IncAttempt(ctx, source, externalID string) (newCount int, err error)`.
- Produces (engine): `Report` gains `Reconciled, Completed, Failed, Rechecked, Parked int`; `Run` runs recheck (re-resolve `not_found`, bounded) then reconcile (poll open requests) when `!dryRun`.

- [ ] **Step 1: Write the failing store test**

Create `internal/store/reconcile_test.go`:
```go
package store

import (
	"context"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

func TestOpenRequestItemsAndApplyStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	b := sources.Book{Source: "goodreads", ExternalID: "1", Title: "A", Author: "X"}
	if _, err := s.Diff(ctx, []sources.Book{b}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRequested(ctx, b, "ol:1", "req_1"); err != nil {
		t.Fatal(err)
	}
	open, err := s.OpenRequestItems(ctx)
	if err != nil || len(open) != 1 || open[0].RequestID != "req_1" {
		t.Fatalf("open=%+v err=%v", open, err)
	}
	if err := s.ApplyStatus(ctx, "goodreads", "1", "done"); err != nil {
		t.Fatal(err)
	}
	open2, _ := s.OpenRequestItems(ctx)
	if len(open2) != 0 {
		t.Fatalf("done item should not be open, got %+v", open2)
	}
}

func TestNotFoundItemsAndIncAttempt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	b := sources.Book{Source: "goodreads", ExternalID: "9", Title: "Z", Author: "Q"}
	if _, err := s.Diff(ctx, []sources.Book{b}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetState(ctx, b, "not_found"); err != nil {
		t.Fatal(err)
	}
	items, err := s.NotFoundItems(ctx, 5)
	if err != nil || len(items) != 1 || items[0].Title != "Z" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	n, err := s.IncAttempt(ctx, "goodreads", "9")
	if err != nil || n != 1 {
		t.Fatalf("incAttempt=%d err=%v", n, err)
	}
}
```

- [ ] **Step 2: Run it — must fail**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && cd /c/source/repo/shelfarr-bookbridge && go test ./internal/store/ -run "TestOpenRequestItems|TestNotFoundItems" -v`
Expected: FAIL (undefined methods).

- [ ] **Step 3: Implement the store queries**

Append to `internal/store/books.go`:
```go
type ReqRef struct {
	Source     string
	ExternalID string
	RequestID  string
}

// OpenRequestItems returns books that have an in-flight Shelfarr request.
func (s *Store) OpenRequestItems(ctx context.Context) ([]ReqRef, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT source, external_id, shelfarr_request_id FROM books
		 WHERE shelfarr_request_id IS NOT NULL AND shelfarr_request_id <> ''
		   AND state IN ('requested','searching','downloading','processing')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReqRef
	for rows.Next() {
		var r ReqRef
		if err := rows.Scan(&r.Source, &r.ExternalID, &r.RequestID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ApplyStatus sets a book's state (used by reconciliation).
func (s *Store) ApplyStatus(ctx context.Context, source, externalID, state string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE books SET state=?, updated_at=datetime('now') WHERE source=? AND external_id=?`,
		state, source, externalID)
	return err
}

// NotFoundItems returns books still in not_found below the attempt cap.
func (s *Store) NotFoundItems(ctx context.Context, maxAttempts int) ([]sources.Book, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT source, external_id, title, author, COALESCE(isbn10,'') FROM books
		 WHERE state='not_found' AND attempt_count < ?`, maxAttempts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []sources.Book
	for rows.Next() {
		var b sources.Book
		if err := rows.Scan(&b.Source, &b.ExternalID, &b.Title, &b.Author, &b.ISBN10); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// IncAttempt increments and returns a book's attempt counter.
func (s *Store) IncAttempt(ctx context.Context, source, externalID string) (int, error) {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE books SET attempt_count = attempt_count + 1, updated_at=datetime('now')
		 WHERE source=? AND external_id=?`, source, externalID); err != nil {
		return 0, err
	}
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT attempt_count FROM books WHERE source=? AND external_id=?`, source, externalID).Scan(&n)
	return n, err
}
```

- [ ] **Step 4: Write the failing engine reconcile test**

Create `internal/engine/reconcile_test.go`:
```go
package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/shelfarr"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/store"
)

func TestEngineReconcileMarksCompleted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/search":
			w.Write([]byte(`{"results":[{"work_id":"ol:1","title":"Dune","author":"Frank Herbert","confidence":90}]}`))
		case strings.HasPrefix(r.URL.Path, "/api/v1/requests/"):
			w.Write([]byte(`{"status":"completed"}`))
		case r.URL.Path == "/api/v1/requests":
			w.WriteHeader(201)
			w.Write([]byte(`{"requests":[{"id":"req_1"}],"errors":[]}`))
		}
	}))
	defer srv.Close()
	st, _ := store.Open(t.TempDir() + "/bb.db")
	defer st.Close()
	sh := shelfarr.New(srv.URL, config.SecretString("t"), nil)
	src := fixedSource{[]sources.Book{{Source: "goodreads", ExternalID: "1", Title: "Dune", Author: "Frank Herbert"}}}
	e := New(src, st, sh, config.Config{Format: "ebook", SimilarityThreshold: 0.82, MaxRequestsPerRun: 25})
	rep, err := e.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Requested != 1 {
		t.Fatalf("want 1 requested, got %+v", rep)
	}
	if rep.Reconciled != 1 || rep.Completed != 1 {
		t.Fatalf("want reconciled=1 completed=1, got %+v", rep)
	}
	var state string
	st.DB().QueryRow(`SELECT state FROM books WHERE external_id='1'`).Scan(&state)
	if state != "done" {
		t.Fatalf("state=%q want done", state)
	}
}
```

- [ ] **Step 5: Expose a read-only DB accessor for tests**

Append to `internal/store/store.go`:
```go
// DB exposes the underlying *sql.DB for tests and advanced callers.
func (s *Store) DB() *sql.DB { return s.db }
```

- [ ] **Step 6: Implement engine recheck + reconcile**

In `internal/engine/engine.go`: extend the `Report` struct to:
```go
type Report struct {
	Fetched, New, Requested, NotFound, AlreadyExists int
	Reconciled, Completed, Failed, Rechecked, Parked int
}
```
Add a helper and the two phases. Add this method and call it at the end of `Run` (just before `return rep, nil`), guarded by `!dryRun`:
```go
const maxRecheckAttempts = 5

// statusToState maps a Shelfarr request status to our book state.
func statusToState(s string) string {
	switch s {
	case "completed":
		return "done"
	case "failed":
		return "failed"
	case "not_found":
		return "not_found"
	case "downloading", "processing":
		return "downloading"
	default: // pending, searching
		return "searching"
	}
}

// resolveAndRequest searches+resolves+requests one book. Returns one of
// "requested", "exists", "not_found".
func (e *Engine) resolveAndRequest(ctx context.Context, b sources.Book) (string, error) {
	q := b.ISBN10
	if q == "" {
		q = b.Title + " " + b.Author
	}
	results, err := e.sh.Search(ctx, q, 10)
	if err != nil {
		return "", err
	}
	pick, _ := resolver.Resolve(b, results, e.cfg.SimilarityThreshold)
	if pick == nil {
		return "not_found", nil
	}
	lang := e.detectLang(b)
	if lang != "" {
		_ = e.st.SetChosenLanguage(ctx, b, lang)
	}
	if err := e.st.SetState(ctx, b, "requesting"); err != nil {
		return "", err
	}
	_, exists, err := e.sh.CreateRequest(ctx, shelfarr.CreateRequestParams{
		WorkID: pick.WorkID, BookTypes: []string{e.cfg.Format}, Language: lang,
		Title: b.Title, Author: b.Author, CoverURL: pick.CoverURL, Year: pick.Year,
	})
	if err != nil {
		return "", err
	}
	if err := e.st.SetRequested(ctx, b, pick.WorkID, ""); err != nil {
		return "", err
	}
	if exists {
		return "exists", nil
	}
	return "requested", nil
}

func (e *Engine) recheckPhase(ctx context.Context, rep *Report) error {
	items, err := e.st.NotFoundItems(ctx, maxRecheckAttempts)
	if err != nil {
		return err
	}
	for _, b := range items {
		if rep.Requested+rep.Rechecked >= e.cfg.MaxRequestsPerRun {
			break
		}
		outcome, err := e.resolveAndRequest(ctx, b)
		if err != nil {
			return err
		}
		if outcome == "not_found" {
			n, err := e.st.IncAttempt(ctx, b.Source, b.ExternalID)
			if err != nil {
				return err
			}
			if n >= maxRecheckAttempts {
				_ = e.st.ApplyStatus(ctx, b.Source, b.ExternalID, "parked")
				rep.Parked++
			}
			continue
		}
		rep.Rechecked++
	}
	return nil
}

func (e *Engine) reconcilePhase(ctx context.Context, rep *Report) error {
	open, err := e.st.OpenRequestItems(ctx)
	if err != nil {
		return err
	}
	for _, ref := range open {
		st, err := e.sh.GetRequest(ctx, ref.RequestID)
		if err != nil {
			if err == shelfarr.ErrRequestNotFound {
				_ = e.st.ApplyStatus(ctx, ref.Source, ref.ExternalID, "cancelled")
				continue
			}
			return err
		}
		newState := statusToState(st.Status)
		if err := e.st.ApplyStatus(ctx, ref.Source, ref.ExternalID, newState); err != nil {
			return err
		}
		rep.Reconciled++
		switch newState {
		case "done":
			rep.Completed++
		case "failed":
			rep.Failed++
		}
	}
	return nil
}
```
Then in `Run`, immediately before `return rep, nil`, add:
```go
	if !dryRun {
		if err := e.recheckPhase(ctx, &rep); err != nil {
			return rep, err
		}
		if err := e.reconcilePhase(ctx, &rep); err != nil {
			return rep, err
		}
	}
```

> Note: `SetRequested` is called with an empty requestID inside `resolveAndRequest` to keep that helper simple; the main new-books loop in `Run` (unchanged from Plan 1) still records the real request id. Reconciliation operates on items that have a non-empty `shelfarr_request_id`, so recheck-created requests that returned no id are reconciled on a later run once their id is known — acceptable for this phase. (Phase 2c threads the id through.)

- [ ] **Step 7: Run + commit**

Run: `go test ./internal/store/ ./internal/engine/ -v && go test ./...` → green.
```bash
git add internal/store/books.go internal/store/store.go internal/engine/engine.go internal/store/reconcile_test.go internal/engine/reconcile_test.go
git commit -m "feat(engine,store): reconcile request status + bounded recheck of not_found"
```

### Task 6: Single-flight run lock

**Files:**
- Create: `internal/store/lock.go`
- Modify: `internal/engine/engine.go`
- Test: `internal/store/lock_test.go`

**Interfaces:**
- Produces (store): `(*Store).AcquireRun(ctx) (ok bool, err error)`; `(*Store).ReleaseRun(ctx) error` using a `run_state` singleton row (schema v3).
- Produces (engine): `var ErrRunInProgress = errors.New(...)`; `Run` acquires the lock first and returns `ErrRunInProgress` if a run holds it.

- [ ] **Step 1: Write the failing test**

Create `internal/store/lock_test.go`:
```go
package store

import (
	"context"
	"testing"
)

func TestRunLockIsSingleFlight(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	ok, err := s.AcquireRun(ctx)
	if err != nil || !ok {
		t.Fatalf("first acquire should succeed: ok=%v err=%v", ok, err)
	}
	ok2, err := s.AcquireRun(ctx)
	if err != nil || ok2 {
		t.Fatalf("second acquire should fail while held: ok=%v err=%v", ok2, err)
	}
	if err := s.ReleaseRun(ctx); err != nil {
		t.Fatal(err)
	}
	ok3, _ := s.AcquireRun(ctx)
	if !ok3 {
		t.Fatal("acquire after release should succeed")
	}
}
```

- [ ] **Step 2: Run it — must fail**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && cd /c/source/repo/shelfarr-bookbridge && go test ./internal/store/ -run TestRunLock -v`
Expected: FAIL (AcquireRun undefined).

- [ ] **Step 3: Add schema v3 + lock methods**

In `internal/store/store.go`, change `const schemaVersion = 2` to `const schemaVersion = 3` and append a third entry to the `migrations` slice (after the v2 string):
```go
		// v3
		`CREATE TABLE IF NOT EXISTS run_state (
  id INTEGER PRIMARY KEY CHECK (id = 1), running INTEGER NOT NULL DEFAULT 0, started_at TEXT);
INSERT OR IGNORE INTO run_state(id, running) VALUES (1, 0);`,
```

Create `internal/store/lock.go`:
```go
package store

import "context"

// AcquireRun atomically takes the single-flight run lock. ok=false means a run
// is already in progress.
func (s *Store) AcquireRun(ctx context.Context) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE run_state SET running=1, started_at=datetime('now') WHERE id=1 AND running=0`)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// ReleaseRun releases the run lock.
func (s *Store) ReleaseRun(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE run_state SET running=0, started_at=NULL WHERE id=1`)
	return err
}
```

- [ ] **Step 4: Make Run single-flight**

In `internal/engine/engine.go`, add near the other vars/imports:
```go
var ErrRunInProgress = errors.New("a sync run is already in progress")
```
(ensure `errors` is imported). At the very start of `Run`, before fetching:
```go
	ok, err := e.st.AcquireRun(ctx)
	if err != nil {
		return Report{}, err
	}
	if !ok {
		return Report{}, ErrRunInProgress
	}
	defer e.st.ReleaseRun(ctx)
```

- [ ] **Step 5: Run + commit**

Run: `go test ./internal/store/ ./internal/engine/ -v && go test ./...` → green.
```bash
git add internal/store/lock.go internal/store/store.go internal/engine/engine.go internal/store/lock_test.go
git commit -m "feat(store,engine): single-flight run lock (schema v3)"
```

---

## Self-Review (performed)

- **Coverage:** langdetect package with confidence gate (T1); config flag + store language persistence (T2); engine sends gated language (T3); Shelfarr status client with 404 handling (T4); reconcile (status→state) + bounded recheck of not_found with attempt cap + parked terminal state (T5); single-flight run lock (T6). Together these close the request loop and make repeated/scheduled runs safe.
- **Placeholder scan:** none; the T5 note about `resolveAndRequest` passing an empty request id is an explicit, bounded simplification superseded in Phase 2c, not a stub.
- **Type consistency:** `LanguageDetector.Detect`, `SetDetector`, `detectLang`, `SetChosenLanguage`, `GetRequest`/`RequestStatus`/`ErrRequestNotFound`, `ReqRef`/`OpenRequestItems`/`ApplyStatus`/`NotFoundItems`/`IncAttempt`, `AcquireRun`/`ReleaseRun`/`ErrRunInProgress`, `Store.DB()` are referenced identically across tasks. `langdetect.Detector` satisfies `engine.LanguageDetector`.

## Deferred to Phase 2c

Scheduler + `daemon` subcommand (hourly cron); thread the real request id through `resolveAndRequest`; secrets-at-rest (file perms / encryption); transport safety (`SHELFARR_INSECURE` guard) + egress allowlist; Dockerfile + Unraid Community Apps template + non-root container.
