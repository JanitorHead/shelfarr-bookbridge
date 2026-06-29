# Shelfarr BookBridge — Phase 1 Core Slice Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A working CLI that reads a Goodreads RSS shelf, resolves each new book against Shelfarr, and creates ebook download requests — with SQLite dedup, `--dry-run`, and per-shelf first-run baseline.

**Architecture:** One Go binary. A `Source` reads books from Goodreads (RSS in this plan), a `store` (SQLite) deduplicates by `(source, book_id)`, a pure `resolver` picks the best Shelfarr search result by self-computed title/author similarity, and a `shelfarr` client submits `POST /api/v1/requests`. The `engine` orchestrates fetch→diff→request phases; the `cmd` layer exposes `sync --dry-run|--apply`.

**Tech Stack:** Go 1.23+, `modernc.org/sqlite` (CGO-free), `database/sql`, `encoding/xml` (RSS), stdlib `net/http` + `flag`. No other deps in this plan.

## Global Constraints

- Go module path: `github.com/JanitorHead/shelfarr-bookbridge`. Go 1.23+.
- SQLite driver: `modernc.org/sqlite` only (CGO-free, single static binary). Open with `_pragma=busy_timeout(5000)` and `_pragma=journal_mode(WAL)`.
- **Identity / dedup key = `(source, external_id)`** where `external_id` = Goodreads `book_id`. `AddedAt` is ordering/baseline metadata only — NEVER part of identity.
- **Shelfarr API:** base `<SHELFARR_URL>/api/v1`; auth header `Authorization: Bearer <shf_ token>`; scopes used: `search:read`, `requests:write`, `requests:read`.
- **`POST /api/v1/requests` is NOT idempotent:** a duplicate returns HTTP 422 with reason in `errors[]` and no existing id. Write a local intent row BEFORE the POST; treat 422-"already exists" as a no-op success.
- **Resolver is a PURE function** (no I/O). Accept a match only if self-computed similarity ≥ `SIMILARITY_THRESHOLD` (default `0.82`). NEVER threshold on Shelfarr `confidence` (corroboration, not relevance) and NEVER hard-gate on `has_ebook` (tri-state; nil = unknown).
- Secrets (`SHELFARR_TOKEN`, `GOODREADS_FEED_KEY`) use the `SecretString` type; never log or serialize the raw value.
- Deterministic runtime, no LLM. TDD: failing test first, minimal impl, commit per task.
- Run tests with `go test ./...`. Every task ends green and committed.

---

## File Structure

| Path | Responsibility |
|---|---|
| `go.mod`, `go.sum` | Module + deps |
| `internal/config/secret.go` | `SecretString` (redacting) |
| `internal/config/config.go` | Env → `Config` |
| `internal/store/store.go` | DB open (pragmas), migrations runner |
| `internal/store/books.go` | `Book` model, `Upsert`, `Diff`, baseline |
| `internal/shelfarr/client.go` | HTTP client, auth, error decoding |
| `internal/shelfarr/search.go` | `Search` + result types |
| `internal/shelfarr/requests.go` | `CreateRequest`, 422 handling |
| `internal/resolver/similarity.go` | normalized title/author similarity |
| `internal/resolver/resolve.go` | pure `Resolve` |
| `internal/sources/source.go` | `Source` interface, `Book` |
| `internal/sources/goodreads/rss.go` | RSS XML parse → `[]Book` |
| `internal/sources/goodreads/source.go` | fetch + RSS `Source` impl |
| `internal/engine/engine.go` | fetch→diff→request phases |
| `cmd/bookbridge/main.go` | CLI: `sync`, flags |
| `cmd/spike-shelfarr/main.go` | Phase 0 live capture tool |

---

## Phase 0 — De-risk

### Task 0.1: Module scaffold

**Files:**
- Create: `go.mod`
- Create: `internal/sources/source.go`

**Interfaces:**
- Produces: module path `github.com/JanitorHead/shelfarr-bookbridge`; `sources.Book` struct; `sources.Source` interface.

- [ ] **Step 1: Init the module**

Run:
```bash
cd /c/source/repo/shelfarr-bookbridge
go mod init github.com/JanitorHead/shelfarr-bookbridge
go get modernc.org/sqlite@latest
```
Expected: `go.mod` created, `modernc.org/sqlite` added.

- [ ] **Step 2: Define the shared Book + Source types**

Create `internal/sources/source.go`:
```go
package sources

import (
	"context"
	"time"
)

// Book is the normalized record produced by every Source.
// Identity is (Source, ExternalID); AddedAt is metadata only.
type Book struct {
	Source     string // e.g. "goodreads"
	ExternalID string // Goodreads book_id — always present
	Title      string
	Author     string
	ISBN10     string // may be empty
	Shelves    []string
	AddedAt    time.Time
	Year       int
	CoverURL   string
}

// Source fetches books from the enabled shelves.
type Source interface {
	Fetch(ctx context.Context, shelves []string) ([]Book, error)
}
```

- [ ] **Step 3: Verify it builds**

Run: `go build ./...`
Expected: no output, exit 0.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum internal/sources/source.go
git commit -m "chore: scaffold module + Source/Book types"
```

### Task 0.2: Shelfarr live round-trip capture tool

**Files:**
- Create: `cmd/spike-shelfarr/main.go`

**Interfaces:**
- Consumes: env `SHELFARR_URL`, `SHELFARR_TOKEN`.
- Produces: printed JSON of a real `search` + `requests/:id` response (saved by hand into `internal/shelfarr/testdata/`).

This is a manual de-risk tool — it confirms the real wire shapes before we code the typed client. It is run once by the owner against the live instance.

- [ ] **Step 1: Write the capture tool**

Create `cmd/spike-shelfarr/main.go`:
```go
package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
)

func main() {
	base := os.Getenv("SHELFARR_URL")
	token := os.Getenv("SHELFARR_TOKEN")
	q := "dune"
	if len(os.Args) > 1 {
		q = os.Args[1]
	}
	u := base + "/api/v1/search?limit=5&q=" + url.QueryEscape(q)
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("HTTP %d\n%s\n", resp.StatusCode, body)
}
```

- [ ] **Step 2: Build it**

Run: `go build ./cmd/spike-shelfarr`
Expected: builds clean.

- [ ] **Step 3: Owner runs it live & saves fixtures**

Run (owner, via `! ...`):
```bash
SHELFARR_URL=https://shelfarr.example SHELFARR_TOKEN=shf_xxx ./spike-shelfarr "dune" > internal/shelfarr/testdata/search_dune.json
```
Expected: HTTP 200 + a `{"results":[...]}` body with a `work_id` field. Save it verbatim. If 401 → token/scope wrong; if 404 → check `/api/v1` prefix.

- [ ] **Step 4: Commit the fixture + tool**

```bash
git add cmd/spike-shelfarr/main.go internal/shelfarr/testdata/search_dune.json
git commit -m "chore: shelfarr live-capture spike + real search fixture"
```

### Task 0.3: Goodreads RSS fetch spike

**Files:**
- Create: `internal/sources/goodreads/testdata/to-read.xml` (captured)

**Interfaces:**
- Produces: a real RSS body fixture used by the parser tests in Task 9.

- [ ] **Step 1: Capture a real shelf feed**

Run (owner, via `! ...`):
```bash
curl -s "https://www.goodreads.com/review/list_rss/<USER_ID>?shelf=to-read" -o internal/sources/goodreads/testdata/to-read.xml
```
Expected: an `<rss>` document whose `<item>`s contain `<book_id>`, `<title>`, `<author_name>`, `<isbn>` (possibly empty). If empty/blocked → the profile is private; append `&key=<FEED_KEY>`.

- [ ] **Step 2: Eyeball the fields**

Run: `grep -m1 -o "<book_id>[^<]*" internal/sources/goodreads/testdata/to-read.xml`
Expected: a numeric id prints — confirms `book_id` presence (our identity key).

- [ ] **Step 3: Commit the fixture**

```bash
git add internal/sources/goodreads/testdata/to-read.xml
git commit -m "chore: capture real Goodreads RSS fixture"
```

---

## Phase 1 — Core Slice

### Task 1: SecretString

**Files:**
- Create: `internal/config/secret.go`
- Test: `internal/config/secret_test.go`

**Interfaces:**
- Produces: `config.SecretString` with `.Reveal() string`, redacting `String()`/`MarshalJSON`.

- [ ] **Step 1: Write the failing test**

Create `internal/config/secret_test.go`:
```go
package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestSecretStringRedacts(t *testing.T) {
	s := SecretString("shf_supersecret")
	if got := s.String(); strings.Contains(got, "supersecret") {
		t.Fatalf("String() leaked secret: %q", got)
	}
	if got := fmt.Sprintf("%v %s", s, s); strings.Contains(got, "supersecret") {
		t.Fatalf("format verbs leaked secret: %q", got)
	}
	b, _ := json.Marshal(struct{ T SecretString }{s})
	if strings.Contains(string(b), "supersecret") {
		t.Fatalf("JSON leaked secret: %s", b)
	}
	if s.Reveal() != "shf_supersecret" {
		t.Fatalf("Reveal() wrong: %q", s.Reveal())
	}
}
```

- [ ] **Step 2: Run it — must fail**

Run: `go test ./internal/config/ -run TestSecretStringRedacts -v`
Expected: FAIL (SecretString undefined).

- [ ] **Step 3: Implement**

Create `internal/config/secret.go`:
```go
package config

// SecretString hides its value from logs, fmt, and JSON. Use Reveal() only at
// the exact point a raw credential is needed (HTTP header, URL).
type SecretString string

const redacted = "***"

func (s SecretString) String() string           { return redacted }
func (s SecretString) GoString() string          { return redacted }
func (s SecretString) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }
func (s SecretString) Reveal() string            { return string(s) }
```

- [ ] **Step 4: Run it — must pass**

Run: `go test ./internal/config/ -run TestSecretStringRedacts -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/secret.go internal/config/secret_test.go
git commit -m "feat(config): redacting SecretString type"
```

### Task 2: Config from env

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: `SecretString`.
- Produces: `config.Config{ ShelfarrURL string; ShelfarrToken SecretString; GoodreadsUserID string; GoodreadsFeedKey SecretString; Shelves []string; Format string; SimilarityThreshold float64; FirstRun string; MaxRequestsPerRun int }` and `config.Load() (Config, error)`.

- [ ] **Step 1: Write the failing test**

Create `internal/config/config_test.go`:
```go
package config

import "testing"

func TestLoadDefaultsAndParsing(t *testing.T) {
	env := map[string]string{
		"SHELFARR_URL":   "https://s.example",
		"SHELFARR_TOKEN": "shf_t",
		"GOODREADS_USER_ID": "42",
		"SHELVES":        "to-read, sci-fi",
	}
	c, err := loadFrom(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if c.ShelfarrURL != "https://s.example" || c.ShelfarrToken.Reveal() != "shf_t" {
		t.Fatalf("bad shelfarr cfg: %+v", c)
	}
	if len(c.Shelves) != 2 || c.Shelves[1] != "sci-fi" {
		t.Fatalf("shelves not trimmed/split: %#v", c.Shelves)
	}
	if c.SimilarityThreshold != 0.82 || c.Format != "ebook" || c.FirstRun != "baseline" || c.MaxRequestsPerRun != 25 {
		t.Fatalf("defaults wrong: %+v", c)
	}
}

func TestLoadMissingRequired(t *testing.T) {
	if _, err := loadFrom(func(string) string { return "" }); err == nil {
		t.Fatal("expected error when SHELFARR_URL missing")
	}
}
```

- [ ] **Step 2: Run it — must fail**

Run: `go test ./internal/config/ -run TestLoad -v`
Expected: FAIL (loadFrom/Config undefined).

- [ ] **Step 3: Implement**

Create `internal/config/config.go`:
```go
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	ShelfarrURL         string
	ShelfarrToken       SecretString
	GoodreadsUserID     string
	GoodreadsFeedKey    SecretString
	Shelves             []string
	Format              string
	SimilarityThreshold float64
	FirstRun            string
	MaxRequestsPerRun   int
}

func Load() (Config, error) { return loadFrom(os.Getenv) }

func loadFrom(get func(string) string) (Config, error) {
	c := Config{
		ShelfarrURL:         get("SHELFARR_URL"),
		ShelfarrToken:       SecretString(get("SHELFARR_TOKEN")),
		GoodreadsUserID:     get("GOODREADS_USER_ID"),
		GoodreadsFeedKey:    SecretString(get("GOODREADS_FEED_KEY")),
		Shelves:             splitCSV(get("SHELVES")),
		Format:              orDefault(get("FORMAT"), "ebook"),
		SimilarityThreshold: 0.82,
		FirstRun:            orDefault(get("FIRST_RUN"), "baseline"),
		MaxRequestsPerRun:   25,
	}
	if v := get("SIMILARITY_THRESHOLD"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			c.SimilarityThreshold = f
		}
	}
	if v := get("MAX_REQUESTS_PER_RUN"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.MaxRequestsPerRun = n
		}
	}
	if c.ShelfarrURL == "" || c.ShelfarrToken.Reveal() == "" {
		return c, fmt.Errorf("SHELFARR_URL and SHELFARR_TOKEN are required")
	}
	return c, nil
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
```

- [ ] **Step 4: Run it — must pass**

Run: `go test ./internal/config/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): load Config from env with defaults"
```

### Task 3: Store open + migrations

**Files:**
- Create: `internal/store/store.go`
- Test: `internal/store/store_test.go`

**Interfaces:**
- Produces: `store.Open(path string) (*Store, error)`, `(*Store).Close()`, internal `migrate()` creating the schema; `PRAGMA user_version` tracking.

- [ ] **Step 1: Write the failing test**

Create `internal/store/store_test.go`:
```go
package store

import (
	"path/filepath"
	"testing"
)

func TestOpenMigratesAndIsIdempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bb.db")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	var ver int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != schemaVersion {
		t.Fatalf("user_version = %d, want %d", ver, schemaVersion)
	}
	// reopening must not error (idempotent migration)
	s.Close()
	s2, err := Open(p)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	// books table must exist
	if _, err := s2.db.Exec(`INSERT INTO books(source,external_id,title,author,state) VALUES('goodreads','1','t','a','new')`); err != nil {
		t.Fatalf("insert into books: %v", err)
	}
}
```

- [ ] **Step 2: Run it — must fail**

Run: `go test ./internal/store/ -run TestOpen -v`
Expected: FAIL (Open undefined).

- [ ] **Step 3: Implement**

Create `internal/store/store.go`:
```go
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

const schemaVersion = 1

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // single serialized writer
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	var ver int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&ver); err != nil {
		return err
	}
	if ver > schemaVersion {
		return fmt.Errorf("db schema v%d is newer than supported v%d; upgrade BookBridge", ver, schemaVersion)
	}
	if ver == schemaVersion {
		return nil
	}
	const ddl = `
CREATE TABLE IF NOT EXISTS books (
  source TEXT NOT NULL,
  external_id TEXT NOT NULL,
  title TEXT NOT NULL,
  author TEXT NOT NULL,
  isbn10 TEXT,
  year INTEGER,
  cover_url TEXT,
  added_at TEXT,
  first_seen_at TEXT NOT NULL DEFAULT (datetime('now')),
  state TEXT NOT NULL,
  work_id TEXT,
  chosen_language TEXT,
  chosen_format TEXT,
  shelfarr_request_id TEXT,
  attempt_count INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (source, external_id)
);
CREATE INDEX IF NOT EXISTS idx_books_state ON books(state);
CREATE TABLE IF NOT EXISTS shelf_config (
  shelf TEXT PRIMARY KEY,
  enabled INTEGER NOT NULL DEFAULT 1,
  baselined_at TEXT,
  format TEXT,
  language TEXT
);
`
	if _, err := s.db.Exec(ddl); err != nil {
		return err
	}
	_, err := s.db.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion))
	return err
}
```

- [ ] **Step 4: Run it — must pass**

Run: `go test ./internal/store/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/store.go internal/store/store_test.go
git commit -m "feat(store): open SQLite with WAL + idempotent migration"
```

### Task 4: Book upsert, Diff, baseline

**Files:**
- Create: `internal/store/books.go`
- Test: `internal/store/books_test.go`

**Interfaces:**
- Consumes: `sources.Book`, `*Store`.
- Produces: `(*Store).Diff(ctx, books []sources.Book) (newBooks []sources.Book, err error)` — returns only books not already known; records unknown books with `state='new'`. `(*Store).BaselineShelf(ctx, shelf string)` marks current `new` rows for that shelf as `state='baseline'`. `(*Store).SetState(ctx, b sources.Book, state string)`.

- [ ] **Step 1: Write the failing test**

Create `internal/store/books_test.go`:
```go
package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestDiffReturnsOnlyUnknownAndPersists(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	books := []sources.Book{
		{Source: "goodreads", ExternalID: "1", Title: "A", Author: "X"},
		{Source: "goodreads", ExternalID: "2", Title: "B", Author: "Y"},
	}
	got, err := s.Diff(ctx, books)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("first Diff should return 2 new, got %d", len(got))
	}
	// second time: nothing new
	got2, err := s.Diff(ctx, books)
	if err != nil {
		t.Fatal(err)
	}
	if len(got2) != 0 {
		t.Fatalf("second Diff should return 0 new, got %d", len(got2))
	}
}

func TestBaselineExcludesFromFutureAction(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	b := sources.Book{Source: "goodreads", ExternalID: "1", Title: "A", Author: "X", Shelves: []string{"to-read"}}
	if _, err := s.Diff(ctx, []sources.Book{b}); err != nil {
		t.Fatal(err)
	}
	if err := s.BaselineShelf(ctx, "to-read"); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := s.db.QueryRow(`SELECT state FROM books WHERE external_id='1'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "baseline" {
		t.Fatalf("state = %q, want baseline", state)
	}
}
```

- [ ] **Step 2: Run it — must fail**

Run: `go test ./internal/store/ -run "TestDiff|TestBaseline" -v`
Expected: FAIL (Diff/BaselineShelf undefined).

- [ ] **Step 3: Implement**

Create `internal/store/books.go`:
```go
package store

import (
	"context"
	"strings"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

// Diff records any unseen books (state='new') and returns only those that were
// not already known. Known books are left untouched. Identity = (source, external_id).
func (s *Store) Diff(ctx context.Context, books []sources.Book) ([]sources.Book, error) {
	var out []sources.Book
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, b := range books {
		var exists int
		err := tx.QueryRowContext(ctx,
			`SELECT 1 FROM books WHERE source=? AND external_id=?`, b.Source, b.ExternalID).Scan(&exists)
		if err == nil {
			continue // already known
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO books(source,external_id,title,author,isbn10,year,cover_url,added_at,state,chosen_format)
			 VALUES(?,?,?,?,?,?,?,?, 'new', ?)`,
			b.Source, b.ExternalID, b.Title, b.Author, b.ISBN10, b.Year, b.CoverURL,
			b.AddedAt.Format("2006-01-02T15:04:05Z07:00"), strings.Join(b.Shelves, ",")); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// BaselineShelf marks current 'new' books whose shelves contain `shelf` as
// 'baseline' so the first run does not mass-request an existing backlog.
func (s *Store) BaselineShelf(ctx context.Context, shelf string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE books SET state='baseline', updated_at=datetime('now')
		 WHERE state='new' AND (','||chosen_format||',') LIKE ?`, "%,"+shelf+",%")
	// chosen_format temporarily carries the comma-joined shelves from Diff; see note.
	return err
}

// SetState transitions a book's lifecycle state.
func (s *Store) SetState(ctx context.Context, b sources.Book, state string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE books SET state=?, updated_at=datetime('now') WHERE source=? AND external_id=?`,
		state, b.Source, b.ExternalID)
	return err
}
```

> Note: this slice stores the comma-joined shelves in `chosen_format` purely to keep the schema minimal for Plan 1; Plan 2 introduces the `book_shelves` table and moves shelf membership there. The `LIKE '%,shelf,%'` match is exact-token safe because of the surrounding commas.

- [ ] **Step 4: Run it — must pass**

Run: `go test ./internal/store/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/books.go internal/store/books_test.go
git commit -m "feat(store): Diff dedup + per-shelf baseline + SetState"
```

### Task 5: Shelfarr client + Search

**Files:**
- Create: `internal/shelfarr/client.go`
- Create: `internal/shelfarr/search.go`
- Test: `internal/shelfarr/search_test.go`

**Interfaces:**
- Consumes: `config.SecretString`.
- Produces: `shelfarr.New(baseURL string, token config.SecretString, hc *http.Client) *Client`; `SearchResult{ WorkID, Title, Author string; Year int; Confidence *int; HasEbook *bool; CoverURL string }`; `(*Client).Search(ctx, q string, limit int) ([]SearchResult, error)`.

- [ ] **Step 1: Write the failing test**

Create `internal/shelfarr/search_test.go`:
```go
package shelfarr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
)

func TestSearchParsesResultsAndSendsAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer shf_t" {
			t.Errorf("missing/wrong auth header: %q", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/api/v1/search" || r.URL.Query().Get("q") != "dune" {
			t.Errorf("bad request: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		w.Write([]byte(`{"results":[
			{"work_id":"openlibrary:OL1W","title":"Dune","author":"Frank Herbert","year":1965,"confidence":90,"has_ebook":true},
			{"work_id":"google_books:abc","title":"Dune Messiah","author":"Frank Herbert","confidence":null}
		]}`))
	}))
	defer srv.Close()
	c := New(srv.URL, config.SecretString("shf_t"), srv.Client())
	res, err := c.Search(context.Background(), "dune", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0].WorkID != "openlibrary:OL1W" || *res[0].Confidence != 90 || !*res[0].HasEbook {
		t.Fatalf("bad parse: %+v", res)
	}
	if res[1].Confidence != nil {
		t.Fatalf("expected nil confidence, got %v", *res[1].Confidence)
	}
}
```

- [ ] **Step 2: Run it — must fail**

Run: `go test ./internal/shelfarr/ -run TestSearch -v`
Expected: FAIL (New/Search undefined).

- [ ] **Step 3: Implement the client**

Create `internal/shelfarr/client.go`:
```go
package shelfarr

import (
	"net/http"
	"time"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
)

type Client struct {
	base  string
	token config.SecretString
	hc    *http.Client
}

func New(baseURL string, token config.SecretString, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{base: baseURL, token: token, hc: hc}
}

func (c *Client) newReq(method, path string) (*http.Request, error) {
	req, err := http.NewRequest(method, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token.Reveal())
	req.Header.Set("Accept", "application/json")
	return req, nil
}
```

- [ ] **Step 4: Implement Search**

Create `internal/shelfarr/search.go`:
```go
package shelfarr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
)

type SearchResult struct {
	WorkID     string `json:"work_id"`
	Title      string `json:"title"`
	Author     string `json:"author"`
	Year       int    `json:"year"`
	Confidence *int   `json:"confidence"`
	HasEbook   *bool  `json:"has_ebook"`
	CoverURL   string `json:"cover_url"`
}

func (c *Client) Search(ctx context.Context, q string, limit int) ([]SearchResult, error) {
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	req, err := c.newReq("GET", "/api/v1/search?limit="+strconv.Itoa(limit)+"&q="+url.QueryEscape(q))
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("shelfarr search: HTTP %d: %s", resp.StatusCode, body)
	}
	var out struct {
		Results []SearchResult `json:"results"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("shelfarr search decode: %w", err)
	}
	return out.Results, nil
}
```

- [ ] **Step 5: Run it — must pass**

Run: `go test ./internal/shelfarr/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/shelfarr/client.go internal/shelfarr/search.go internal/shelfarr/search_test.go
git commit -m "feat(shelfarr): client + Search with auth"
```

### Task 6: Shelfarr CreateRequest with 422 handling

**Files:**
- Create: `internal/shelfarr/requests.go`
- Test: `internal/shelfarr/requests_test.go`

**Interfaces:**
- Produces: `CreateRequestParams{ WorkID string; BookTypes []string; Language string; Title, Author, CoverURL string; Year int }`; `(*Client).CreateRequest(ctx, p CreateRequestParams) (requestID string, alreadyExists bool, err error)`.

- [ ] **Step 1: Write the failing test**

Create `internal/shelfarr/requests_test.go`:
```go
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
```

- [ ] **Step 2: Run it — must fail**

Run: `go test ./internal/shelfarr/ -run TestCreateRequest -v`
Expected: FAIL (CreateRequest undefined).

- [ ] **Step 3: Implement**

Create `internal/shelfarr/requests.go`:
```go
package shelfarr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type CreateRequestParams struct {
	WorkID    string   `json:"work_id"`
	BookTypes []string `json:"book_types"`
	Language  string   `json:"language,omitempty"`
	Title     string   `json:"title,omitempty"`
	Author    string   `json:"author,omitempty"`
	CoverURL  string   `json:"cover_url,omitempty"`
	Year      int      `json:"year,omitempty"`
}

// CreateRequest POSTs a request. A duplicate (HTTP 422 whose error mentions an
// existing request / already in library) is reported as alreadyExists=true with
// no error — the caller resolves the existing request separately.
func (c *Client) CreateRequest(ctx context.Context, p CreateRequestParams) (string, bool, error) {
	payload, err := json.Marshal(p)
	if err != nil {
		return "", false, err
	}
	req, err := c.newReq("POST", "/api/v1/requests")
	if err != nil {
		return "", false, err
	}
	req.Body = io.NopCloser(bytes.NewReader(payload))
	req.ContentLength = int64(len(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(req.WithContext(ctx))
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var parsed struct {
		Requests []struct {
			ID json.RawMessage `json:"id"`
		} `json:"requests"`
		Errors []string `json:"errors"`
	}
	_ = json.Unmarshal(body, &parsed)

	if resp.StatusCode == 422 {
		joined := strings.ToLower(strings.Join(parsed.Errors, " "))
		if strings.Contains(joined, "already") {
			return "", true, nil
		}
		return "", false, fmt.Errorf("shelfarr create 422: %s", body)
	}
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return "", false, fmt.Errorf("shelfarr create: HTTP %d: %s", resp.StatusCode, body)
	}
	if len(parsed.Requests) == 0 {
		return "", false, fmt.Errorf("shelfarr create: no request in response: %s", body)
	}
	id := strings.Trim(string(parsed.Requests[0].ID), `"`)
	return id, false, nil
}
```

- [ ] **Step 4: Run it — must pass**

Run: `go test ./internal/shelfarr/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/shelfarr/requests.go internal/shelfarr/requests_test.go
git commit -m "feat(shelfarr): CreateRequest with 422-duplicate handling"
```

### Task 7: Resolver similarity

**Files:**
- Create: `internal/resolver/similarity.go`
- Test: `internal/resolver/similarity_test.go`

**Interfaces:**
- Produces: `resolver.Similarity(a, b string) float64` (0..1, normalized trigram Dice coefficient).

- [ ] **Step 1: Write the failing test**

Create `internal/resolver/similarity_test.go`:
```go
package resolver

import "testing"

func TestSimilarity(t *testing.T) {
	if s := Similarity("Dune", "Dune"); s < 0.999 {
		t.Fatalf("identical should be 1.0, got %f", s)
	}
	if s := Similarity("The Name of the Wind", "Name of the Wind"); s < 0.7 {
		t.Fatalf("close titles should score high, got %f", s)
	}
	if s := Similarity("Dune", "War and Peace"); s > 0.2 {
		t.Fatalf("unrelated should score low, got %f", s)
	}
	// normalization: case + accents + punctuation ignored
	if s := Similarity("El Nombre del Viento", "el nombre del viento!"); s < 0.95 {
		t.Fatalf("normalization failed, got %f", s)
	}
}
```

- [ ] **Step 2: Run it — must fail**

Run: `go test ./internal/resolver/ -run TestSimilarity -v`
Expected: FAIL (Similarity undefined).

- [ ] **Step 3: Implement**

Create `internal/resolver/similarity.go`:
```go
package resolver

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// normalize lowercases, strips accents and non-alphanumerics, collapses spaces.
func normalize(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, _ := transform.String(t, s)
	var b strings.Builder
	prevSpace := false
	for _, r := range strings.ToLower(out) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevSpace = false
		case !prevSpace:
			b.WriteRune(' ')
			prevSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

func trigrams(s string) map[string]struct{} {
	s = "  " + s + "  "
	m := make(map[string]struct{})
	r := []rune(s)
	for i := 0; i+3 <= len(r); i++ {
		m[string(r[i:i+3])] = struct{}{}
	}
	return m
}

// Similarity is the Dice coefficient of character trigrams after normalization.
func Similarity(a, b string) float64 {
	na, nb := normalize(a), normalize(b)
	if na == "" || nb == "" {
		return 0
	}
	if na == nb {
		return 1
	}
	ta, tb := trigrams(na), trigrams(nb)
	inter := 0
	for g := range ta {
		if _, ok := tb[g]; ok {
			inter++
		}
	}
	return 2 * float64(inter) / float64(len(ta)+len(tb))
}
```

- [ ] **Step 4: Add the text dependency, run, must pass**

Run:
```bash
go get golang.org/x/text@latest
go test ./internal/resolver/ -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/resolver/similarity.go internal/resolver/similarity_test.go go.mod go.sum
git commit -m "feat(resolver): normalized trigram similarity"
```

### Task 8: Pure Resolve

**Files:**
- Create: `internal/resolver/resolve.go`
- Test: `internal/resolver/resolve_test.go`

**Interfaces:**
- Consumes: `sources.Book`, `shelfarr.SearchResult`, `Similarity`.
- Produces: `resolver.Pick{ WorkID string; Title, Author string; Year int; CoverURL string; Score float64 }`; `resolver.Resolve(b sources.Book, results []shelfarr.SearchResult, threshold float64) (*Pick, string)` — returns the best result whose combined title+author similarity ≥ threshold, else `(nil, reason)`. `confidence` is only a tiebreaker; `has_ebook` is never a hard gate.

- [ ] **Step 1: Write the failing test**

Create `internal/resolver/resolve_test.go`:
```go
package resolver

import (
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/shelfarr"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

func ci(i int) *int { return &i }

func TestResolvePicksBestAboveThreshold(t *testing.T) {
	b := sources.Book{Title: "Dune", Author: "Frank Herbert"}
	res := []shelfarr.SearchResult{
		{WorkID: "a", Title: "Dune Messiah", Author: "Frank Herbert", Confidence: ci(70)},
		{WorkID: "b", Title: "Dune", Author: "Frank Herbert", Confidence: ci(70)},
	}
	pick, reason := Resolve(b, res, 0.82)
	if pick == nil {
		t.Fatalf("expected a pick, reason=%s", reason)
	}
	if pick.WorkID != "b" {
		t.Fatalf("expected exact-title work b, got %q", pick.WorkID)
	}
}

func TestResolveTiebreakOnConfidence(t *testing.T) {
	b := sources.Book{Title: "Dune", Author: "Frank Herbert"}
	res := []shelfarr.SearchResult{
		{WorkID: "low", Title: "Dune", Author: "Frank Herbert", Confidence: ci(70)},
		{WorkID: "high", Title: "Dune", Author: "Frank Herbert", Confidence: ci(100)},
	}
	pick, _ := Resolve(b, res, 0.82)
	if pick == nil || pick.WorkID != "high" {
		t.Fatalf("equal similarity should break on confidence -> high, got %+v", pick)
	}
}

func TestResolveNotFoundBelowThreshold(t *testing.T) {
	b := sources.Book{Title: "Dune", Author: "Frank Herbert"}
	res := []shelfarr.SearchResult{{WorkID: "x", Title: "War and Peace", Author: "Tolstoy"}}
	pick, reason := Resolve(b, res, 0.82)
	if pick != nil {
		t.Fatalf("expected not_found, got %+v", pick)
	}
	if reason == "" {
		t.Fatal("expected a reason string")
	}
}
```

- [ ] **Step 2: Run it — must fail**

Run: `go test ./internal/resolver/ -run TestResolve -v`
Expected: FAIL (Resolve/Pick undefined).

- [ ] **Step 3: Implement**

Create `internal/resolver/resolve.go`:
```go
package resolver

import (
	"fmt"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/shelfarr"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

type Pick struct {
	WorkID   string
	Title    string
	Author   string
	Year     int
	CoverURL string
	Score    float64
}

// Resolve is pure: it selects the search result whose combined title+author
// similarity to the book is highest and >= threshold. Ties break on Shelfarr
// confidence (corroboration), then on result order. has_ebook is NOT a gate.
func Resolve(b sources.Book, results []shelfarr.SearchResult, threshold float64) (*Pick, string) {
	var best *Pick
	var bestConf int
	for _, r := range results {
		score := 0.7*Similarity(b.Title, r.Title) + 0.3*Similarity(b.Author, r.Author)
		if score < threshold {
			continue
		}
		conf := 0
		if r.Confidence != nil {
			conf = *r.Confidence
		}
		if best == nil || score > best.Score || (score == best.Score && conf > bestConf) {
			best = &Pick{WorkID: r.WorkID, Title: r.Title, Author: r.Author, Year: r.Year, CoverURL: r.CoverURL, Score: score}
			bestConf = conf
		}
	}
	if best == nil {
		return nil, fmt.Sprintf("no result >= similarity %.2f for %q by %q", threshold, b.Title, b.Author)
	}
	return best, ""
}
```

- [ ] **Step 4: Run it — must pass**

Run: `go test ./internal/resolver/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/resolver/resolve.go internal/resolver/resolve_test.go
git commit -m "feat(resolver): pure Resolve with similarity gate + confidence tiebreak"
```

### Task 9: Goodreads RSS parser

**Files:**
- Create: `internal/sources/goodreads/rss.go`
- Test: `internal/sources/goodreads/rss_test.go`

**Interfaces:**
- Consumes: `sources.Book`.
- Produces: `goodreads.parseRSS(data []byte, shelf string) ([]sources.Book, error)` — decodes entities, fills `ExternalID` from `book_id`.

- [ ] **Step 1: Write the failing test**

Create `internal/sources/goodreads/rss_test.go`:
```go
package goodreads

import "testing"

const sampleRSS = `<?xml version="1.0"?><rss><channel>
<item>
 <book_id>12345</book_id>
 <title>El Nombre del Viento &amp; Other Tales</title>
 <author_name>Patrick Rothfuss</author_name>
 <isbn>8401352835</isbn>
</item>
<item>
 <book_id>67890</book_id>
 <title>The Wise Man&apos;s Fear</title>
 <author_name>Patrick Rothfuss</author_name>
 <isbn></isbn>
</item>
</channel></rss>`

func TestParseRSS(t *testing.T) {
	books, err := parseRSS([]byte(sampleRSS), "to-read")
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 2 {
		t.Fatalf("want 2 books, got %d", len(books))
	}
	if books[0].ExternalID != "12345" || books[0].Source != "goodreads" {
		t.Fatalf("bad identity: %+v", books[0])
	}
	if books[0].Title != "El Nombre del Viento & Other Tales" {
		t.Fatalf("entities not decoded: %q", books[0].Title)
	}
	if books[1].ISBN10 != "" {
		t.Fatalf("empty isbn should stay empty, got %q", books[1].ISBN10)
	}
	if books[0].Shelves[0] != "to-read" {
		t.Fatalf("shelf not tagged: %v", books[0].Shelves)
	}
}
```

- [ ] **Step 2: Run it — must fail**

Run: `go test ./internal/sources/goodreads/ -run TestParseRSS -v`
Expected: FAIL (parseRSS undefined).

- [ ] **Step 3: Implement**

Create `internal/sources/goodreads/rss.go`:
```go
package goodreads

import (
	"encoding/xml"
	"strings"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

type rssItem struct {
	BookID string `xml:"book_id"`
	Title  string `xml:"title"`
	Author string `xml:"author_name"`
	ISBN   string `xml:"isbn"`
}

type rssDoc struct {
	Items []rssItem `xml:"channel>item"`
}

// parseRSS decodes a Goodreads shelf RSS feed. encoding/xml resolves standard
// entities and does not expand external entities (safe by default).
func parseRSS(data []byte, shelf string) ([]sources.Book, error) {
	var doc rssDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	out := make([]sources.Book, 0, len(doc.Items))
	for _, it := range doc.Items {
		id := strings.TrimSpace(it.BookID)
		if id == "" {
			continue // no identity — skip
		}
		out = append(out, sources.Book{
			Source:     "goodreads",
			ExternalID: id,
			Title:      strings.TrimSpace(it.Title),
			Author:     strings.TrimSpace(it.Author),
			ISBN10:     strings.TrimSpace(it.ISBN),
			Shelves:    []string{shelf},
		})
	}
	return out, nil
}
```

- [ ] **Step 4: Run it — must pass**

Run: `go test ./internal/sources/goodreads/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/sources/goodreads/rss.go internal/sources/goodreads/rss_test.go
git commit -m "feat(goodreads): RSS parser -> Book"
```

### Task 10: Goodreads RSS Source (fetch + cap detection)

**Files:**
- Create: `internal/sources/goodreads/source.go`
- Test: `internal/sources/goodreads/source_test.go`

**Interfaces:**
- Consumes: `config.SecretString`, `parseRSS`, `sources.Book`.
- Produces: `goodreads.NewRSSSource(userID string, feedKey config.SecretString, base string, hc *http.Client) *RSSSource` implementing `sources.Source`. Logs a loud warning when a shelf returns exactly 100 (possible truncation). Host is fixed to Goodreads; `base` override is for tests.

- [ ] **Step 1: Write the failing test**

Create `internal/sources/goodreads/source_test.go`:
```go
package goodreads

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
)

func TestRSSSourceFetchSendsKeyAndParses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/review/list_rss/42" {
			t.Errorf("bad path %s", r.URL.Path)
		}
		if r.URL.Query().Get("shelf") != "to-read" || r.URL.Query().Get("key") != "feedkey" {
			t.Errorf("bad query %s", r.URL.RawQuery)
		}
		w.Write([]byte(sampleRSS))
	}))
	defer srv.Close()
	s := NewRSSSource("42", config.SecretString("feedkey"), srv.URL, srv.Client())
	books, err := s.Fetch(context.Background(), []string{"to-read"})
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 2 {
		t.Fatalf("want 2, got %d", len(books))
	}
}
```

- [ ] **Step 2: Run it — must fail**

Run: `go test ./internal/sources/goodreads/ -run TestRSSSourceFetch -v`
Expected: FAIL (NewRSSSource undefined).

- [ ] **Step 3: Implement**

Create `internal/sources/goodreads/source.go`:
```go
package goodreads

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

type RSSSource struct {
	userID  string
	feedKey config.SecretString
	base    string // default https://www.goodreads.com
	hc      *http.Client
}

func NewRSSSource(userID string, feedKey config.SecretString, base string, hc *http.Client) *RSSSource {
	if base == "" {
		base = "https://www.goodreads.com"
	}
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &RSSSource{userID: userID, feedKey: feedKey, base: base, hc: hc}
}

func (s *RSSSource) Fetch(ctx context.Context, shelves []string) ([]sources.Book, error) {
	var all []sources.Book
	for _, shelf := range shelves {
		q := url.Values{"shelf": {shelf}}
		if k := s.feedKey.Reveal(); k != "" {
			q.Set("key", k)
		}
		u := fmt.Sprintf("%s/review/list_rss/%s?%s", s.base, url.PathEscape(s.userID), q.Encode())
		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "shelfarr-bookbridge/0.1 (+self-hosted)")
		resp, err := s.hc.Do(req)
		if err != nil {
			return nil, fmt.Errorf("goodreads fetch %q: %w", shelf, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("goodreads fetch %q: HTTP %d", shelf, resp.StatusCode)
		}
		books, err := parseRSS(body, shelf)
		if err != nil {
			return nil, fmt.Errorf("goodreads parse %q: %w", shelf, err)
		}
		if len(books) == 100 {
			log.Printf("WARNING: shelf %q returned exactly 100 items — RSS caps at 100; books beyond 100 need the cookie-HTML reader (Plan 2)", shelf)
		}
		all = append(all, books...)
	}
	return all, nil
}
```

- [ ] **Step 4: Run it — must pass**

Run: `go test ./internal/sources/goodreads/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/sources/goodreads/source.go internal/sources/goodreads/source_test.go
git commit -m "feat(goodreads): RSS Source with key auth + 100-cap warning"
```

### Task 11: Engine — fetch→diff→request

**Files:**
- Create: `internal/engine/engine.go`
- Test: `internal/engine/engine_test.go`

**Interfaces:**
- Consumes: `sources.Source`, `*store.Store`, `*shelfarr.Client`, `resolver.Resolve`, `config.Config`.
- Produces: `engine.New(src sources.Source, st *store.Store, sh *shelfarr.Client, cfg config.Config) *Engine`; `(*Engine).Run(ctx, dryRun bool) (Report, error)`; `Report{ Fetched, New, Requested, NotFound, AlreadyExists int }`. On `--apply`, writes intent state before POST and records the request id; on dry-run, only counts.

- [ ] **Step 1: Write the failing test**

Create `internal/engine/engine_test.go`:
```go
package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/shelfarr"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/store"
)

type stubSource struct{ books []sources.Book }

func (s stubSource) Fetch(context.Context, []string) ([]sources.Book, error) { return s.books, nil }

func mockShelfarr(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/search" {
			w.Write([]byte(`{"results":[{"work_id":"ol:1","title":"Dune","author":"Frank Herbert","confidence":90}]}`))
			return
		}
		if r.URL.Path == "/api/v1/requests" {
			w.WriteHeader(201)
			w.Write([]byte(`{"requests":[{"id":"req_1"}],"errors":[]}`))
			return
		}
		t.Errorf("unexpected path %s", r.URL.Path)
	}))
}

func newEngine(t *testing.T, src sources.Source, base string) *Engine {
	st, err := store.Open(filepath.Join(t.TempDir(), "bb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	sh := shelfarr.New(base, config.SecretString("shf_t"), nil)
	cfg := config.Config{Format: "ebook", SimilarityThreshold: 0.82, MaxRequestsPerRun: 25}
	return New(src, st, sh, cfg)
}

func TestEngineApplyRequestsNewBook(t *testing.T) {
	srv := mockShelfarr(t)
	defer srv.Close()
	src := stubSource{books: []sources.Book{{Source: "goodreads", ExternalID: "1", Title: "Dune", Author: "Frank Herbert"}}}
	e := newEngine(t, src, srv.URL)
	rep, err := e.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.New != 1 || rep.Requested != 1 || rep.NotFound != 0 {
		t.Fatalf("bad report: %+v", rep)
	}
	// second run: already known -> nothing new
	rep2, _ := e.Run(context.Background(), false)
	if rep2.New != 0 || rep2.Requested != 0 {
		t.Fatalf("second run should be a no-op: %+v", rep2)
	}
}

func TestEngineDryRunRequestsNothing(t *testing.T) {
	srv := mockShelfarr(t)
	defer srv.Close()
	src := stubSource{books: []sources.Book{{Source: "goodreads", ExternalID: "1", Title: "Dune", Author: "Frank Herbert"}}}
	e := newEngine(t, src, srv.URL)
	rep, err := e.Run(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Requested != 0 || rep.New != 1 {
		t.Fatalf("dry-run must request nothing: %+v", rep)
	}
}
```

- [ ] **Step 2: Run it — must fail**

Run: `go test ./internal/engine/ -v`
Expected: FAIL (New/Engine undefined).

- [ ] **Step 3: Implement**

Create `internal/engine/engine.go`:
```go
package engine

import (
	"context"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/resolver"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/shelfarr"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/store"
)

type Engine struct {
	src sources.Source
	st  *store.Store
	sh  *shelfarr.Client
	cfg config.Config
}

type Report struct{ Fetched, New, Requested, NotFound, AlreadyExists int }

func New(src sources.Source, st *store.Store, sh *shelfarr.Client, cfg config.Config) *Engine {
	return &Engine{src: src, st: st, sh: sh, cfg: cfg}
}

func (e *Engine) Run(ctx context.Context, dryRun bool) (Report, error) {
	var rep Report
	books, err := e.src.Fetch(ctx, e.cfg.Shelves)
	if err != nil {
		return rep, err
	}
	rep.Fetched = len(books)

	newBooks, err := e.st.Diff(ctx, books)
	if err != nil {
		return rep, err
	}
	rep.New = len(newBooks)

	for _, b := range newBooks {
		if rep.Requested >= e.cfg.MaxRequestsPerRun {
			break
		}
		q := b.ISBN10
		if q == "" {
			q = b.Title + " " + b.Author
		}
		results, err := e.sh.Search(ctx, q, 10)
		if err != nil {
			return rep, err
		}
		pick, _ := resolver.Resolve(b, results, e.cfg.SimilarityThreshold)
		if pick == nil {
			rep.NotFound++
			if !dryRun {
				_ = e.st.SetState(ctx, b, "not_found")
			}
			continue
		}
		if dryRun {
			rep.Requested++ // would-request count; nothing sent
			continue
		}
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
		if err != nil {
			return rep, err
		}
		if exists {
			rep.AlreadyExists++
		} else {
			rep.Requested++
		}
		_ = e.st.SetRequested(ctx, b, pick.WorkID, id)
	}
	return rep, nil
}
```

- [ ] **Step 4: Add the SetRequested store helper**

Append to `internal/store/books.go`:
```go
// SetRequested records a successful (or already-existing) request.
func (s *Store) SetRequested(ctx context.Context, b sources.Book, workID, requestID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE books SET state='requested', work_id=?, shelfarr_request_id=?, updated_at=datetime('now')
		 WHERE source=? AND external_id=?`, workID, requestID, b.Source, b.ExternalID)
	return err
}
```

- [ ] **Step 5: Run it — must pass**

Run: `go test ./internal/engine/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/engine/engine.go internal/store/books.go internal/engine/engine_test.go
git commit -m "feat(engine): fetch->diff->request with intent rows + dry-run"
```

### Task 12: CLI `sync`

**Files:**
- Create: `cmd/bookbridge/main.go`
- Test: `cmd/bookbridge/main_test.go`

**Interfaces:**
- Consumes: `config.Load`, `store.Open`, `shelfarr.New`, `goodreads.NewRSSSource`, `engine.New`.
- Produces: `bookbridge sync [--dry-run|--apply] [--baseline]`; default is `--dry-run`. `--baseline` marks each enabled shelf's current contents as seen without requesting.

- [ ] **Step 1: Write the failing test (wiring smoke via run())**

Create `cmd/bookbridge/main_test.go`:
```go
package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSyncDryRun(t *testing.T) {
	gr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<?xml version="1.0"?><rss><channel><item><book_id>1</book_id><title>Dune</title><author_name>Frank Herbert</author_name><isbn></isbn></item></channel></rss>`))
	}))
	defer gr.Close()
	sh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"results":[{"work_id":"ol:1","title":"Dune","author":"Frank Herbert","confidence":90}]}`))
	}))
	defer sh.Close()

	env := map[string]string{
		"SHELFARR_URL": sh.URL, "SHELFARR_TOKEN": "shf_t",
		"GOODREADS_USER_ID": "42", "SHELVES": "to-read",
		"BB_DB":             filepath.Join(t.TempDir(), "bb.db"),
		"GOODREADS_BASE":    gr.URL,
	}
	var out strings.Builder
	code := run([]string{"sync", "--dry-run"}, func(k string) string { return env[k] }, &out)
	if code != 0 {
		t.Fatalf("exit %d, out=%s", code, out.String())
	}
	if !strings.Contains(out.String(), "new=1") {
		t.Fatalf("expected new=1 in output, got %s", out.String())
	}
}
```

- [ ] **Step 2: Run it — must fail**

Run: `go test ./cmd/bookbridge/ -v`
Expected: FAIL (run undefined).

- [ ] **Step 3: Implement**

Create `cmd/bookbridge/main.go`:
```go
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/engine"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/shelfarr"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources/goodreads"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/store"
)

func main() { os.Exit(run(os.Args[1:], os.Getenv, os.Stdout)) }

func run(args []string, getenv func(string) string, out io.Writer) int {
	if len(args) == 0 || args[0] != "sync" {
		fmt.Fprintln(out, "usage: bookbridge sync [--dry-run|--apply] [--baseline]")
		return 2
	}
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	fs.SetOutput(out)
	apply := fs.Bool("apply", false, "create requests (default is dry-run)")
	dry := fs.Bool("dry-run", false, "preview only")
	baseline := fs.Bool("baseline", false, "mark current shelf contents as seen, request nothing")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	dryRun := !*apply || *dry

	cfg, err := config.Load2(getenv)
	if err != nil {
		fmt.Fprintln(out, "config error:", err)
		return 1
	}
	st, err := store.Open(orEnv(getenv, "BB_DB", "/config/bookbridge.db"))
	if err != nil {
		fmt.Fprintln(out, "store error:", err)
		return 1
	}
	defer st.Close()

	src := goodreads.NewRSSSource(cfg.GoodreadsUserID, cfg.GoodreadsFeedKey, getenv("GOODREADS_BASE"), nil)
	sh := shelfarr.New(cfg.ShelfarrURL, cfg.ShelfarrToken, nil)
	ctx := context.Background()

	if *baseline {
		books, err := src.Fetch(ctx, cfg.Shelves)
		if err != nil {
			fmt.Fprintln(out, "fetch error:", err)
			return 1
		}
		if _, err := st.Diff(ctx, books); err != nil {
			fmt.Fprintln(out, "diff error:", err)
			return 1
		}
		for _, shelf := range cfg.Shelves {
			if err := st.BaselineShelf(ctx, shelf); err != nil {
				fmt.Fprintln(out, "baseline error:", err)
				return 1
			}
		}
		fmt.Fprintln(out, "baseline complete")
		return 0
	}

	e := engine.New(src, st, sh, cfg)
	rep, err := e.Run(ctx, dryRun)
	if err != nil {
		fmt.Fprintln(out, "run error:", err)
		return 1
	}
	mode := "apply"
	if dryRun {
		mode = "dry-run"
	}
	fmt.Fprintf(out, "[%s] fetched=%d new=%d requested=%d not_found=%d already_exists=%d\n",
		mode, rep.Fetched, rep.New, rep.Requested, rep.NotFound, rep.AlreadyExists)
	return 0
}

func orEnv(get func(string) string, k, def string) string {
	if v := get(k); v != "" {
		return v
	}
	return def
}
```

- [ ] **Step 4: Add `config.Load2` (env-func variant) to config**

Append to `internal/config/config.go`:
```go
// Load2 loads config from a custom getenv (used by the CLI/tests).
func Load2(get func(string) string) (Config, error) { return loadFrom(get) }
```

- [ ] **Step 5: Run it — must pass**

Run: `go test ./cmd/bookbridge/ -v && go test ./...`
Expected: PASS (all packages green).

- [ ] **Step 6: Commit**

```bash
git add cmd/bookbridge/main.go cmd/bookbridge/main_test.go internal/config/config.go
git commit -m "feat(cli): bookbridge sync with --dry-run/--apply/--baseline"
```

---

## Self-Review (performed)

- **Spec coverage (Phase-1 core slice):** RSS read (T9–T10), dedup by `(source,book_id)` (T4), pure resolver with similarity gate + confidence-tiebreak, no has_ebook gate (T7–T8), Shelfarr search (T5) + non-idempotent request with 422-as-noop + intent-row-before-POST (T6, T11), per-shelf baseline + dry-run default (T4, T12), secret redaction (T1), CGO-free SQLite + WAL + idempotent migration with newer-schema fail-closed (T3). Phase-0 de-risk fixtures (T0.1–0.3). **Deferred to Plan 2 (noted):** cookie-HTML >100 reader, langdetect, status reconciliation + recheck, scheduler/daemon, full security hardening (secrets-at-rest encryption, transport/egress), `book_shelves` table, Docker/Unraid, GUI.
- **Placeholder scan:** none — every step has runnable code/commands.
- **Type consistency:** `sources.Book`, `shelfarr.SearchResult`/`CreateRequestParams`, `resolver.Pick`/`Resolve`, `store.Diff/SetState/SetRequested/BaselineShelf`, `engine.Report` are referenced with identical signatures across tasks. The `chosen_format`-carries-shelves shortcut in T4 is explicitly flagged and superseded in Plan 2.

---

## Plan 2 (next, separate document) will cover

Cookie-HTML reader for private >100 shelves (auth decorator + HTML parser + RSS↔HTML ExternalID-equivalence contract test), `book_shelves` table + multi-shelf precedence, langdetect (lingua-go) wired into the request language, status reconciliation + bounded recheck + `parked` state, single-flight run lock + `Clock` injection, scheduler/daemon mode, secrets-at-rest encryption + transport/egress hardening, programmable mock-Shelfarr status progression, Dockerfile + Unraid Community Apps template.
