# Shelfarr BookBridge — Phase 2a: Cookie-HTML Reader + Data Model Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `bookbridge sync` work for a private Goodreads shelf with >100 items by adding an authenticated cookie-HTML reader, and clean up the data model so shelf membership lives in its own table.

**Architecture:** A new HTML strategy parses Goodreads' `review/list` table (paginated, cookie-authenticated, sign-in-wall detected) into the same `sources.Book`. A composite Goodreads source picks HTML when a session cookie is configured, else RSS. A `book_shelves` table replaces the Plan-1 shortcut of stuffing shelves into `chosen_format`. An invariant contract test asserts RSS and HTML emit identical `ExternalID`s for the same shelf.

**Tech Stack:** Go 1.23+, `github.com/PuerkitoBio/goquery` (HTML), existing stack. goquery is already fetched.

## Global Constraints

- Same as Phase 1: module `github.com/JanitorHead/shelfarr-bookbridge`; `modernc.org/sqlite`; identity = `(source, external_id=book_id)`; `SecretString` for secrets; TDD; `go test ./...` green per task; commit per task with `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>` trailer.
- The Goodreads HTML list route is login-gated even for public profiles (verified 2026-06-29): the HTML reader REQUIRES a session cookie. RSS (≤100) needs no cookie.
- **ExternalID must be byte-identical across the RSS and HTML parsers** for the same book (both = the numeric Goodreads `book_id`). This is a hard invariant (Task 4 contract test).
- HTML pagination: `?shelf=<slug>&per_page=100&page=N&print=true`; stop when a page yields 0 rows or shows the empty sentinel `div.greyText.nocontent.stacked`.
- Detect the sign-in wall (`<title>` contains "Sign in", or a `#signedOutBanner`/`#choices` sign-in element) → return a typed `ErrCookieExpired`, never treat as an empty shelf.

---

## File Structure

| Path | Responsibility | Change |
|---|---|---|
| `internal/store/store.go` | stepwise migrations incl. `book_shelves` | Modify |
| `internal/store/books.go` | `Diff`/`BaselineShelf` use `book_shelves` | Modify |
| `internal/sources/goodreads/html.go` | parse review/list HTML table | Create |
| `internal/sources/goodreads/htmlsource.go` | cookie fetch + pagination + sign-in detection | Create |
| `internal/sources/goodreads/source.go` | `NewSource` composite selector | Modify |
| `internal/config/config.go` | `GoodreadsCookie` field | Modify |
| `cmd/bookbridge/main.go` | pick HTML vs RSS source | Modify |

---

### Task 1: `book_shelves` table + stepwise migrations

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/store/books.go`
- Test: `internal/store/shelves_test.go`

**Interfaces:**
- Produces: schema v2 with `book_shelves(source, external_id, shelf)`; `Diff` writes shelves there (no longer into `chosen_format`); `BaselineShelf(ctx, shelf)` joins `book_shelves`. `(*Store).ShelvesOf(ctx, source, externalID) ([]string, error)`.

- [ ] **Step 1: Write the failing test**

Create `internal/store/shelves_test.go`:
```go
package store

import (
	"context"
	"sort"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

func TestDiffPopulatesBookShelves(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	b := sources.Book{Source: "goodreads", ExternalID: "1", Title: "A", Author: "X", Shelves: []string{"to-read", "sci-fi"}}
	if _, err := s.Diff(ctx, []sources.Book{b}); err != nil {
		t.Fatal(err)
	}
	got, err := s.ShelvesOf(ctx, "goodreads", "1")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	if len(got) != 2 || got[0] != "sci-fi" || got[1] != "to-read" {
		t.Fatalf("book_shelves = %v, want [sci-fi to-read]", got)
	}
}

func TestBaselineUsesBookShelves(t *testing.T) {
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
		t.Fatalf("state=%q, want baseline", state)
	}
}

func TestSchemaVersionIsTwo(t *testing.T) {
	s := newTestStore(t)
	var ver int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&ver); err != nil {
		t.Fatal(err)
	}
	if ver != schemaVersion || schemaVersion != 2 {
		t.Fatalf("user_version=%d schemaVersion=%d, want 2", ver, schemaVersion)
	}
}
```

- [ ] **Step 2: Run it — must fail**

Run (Bash): `export PATH="/c/Program Files/Go/bin:$PATH" && cd /c/source/repo/shelfarr-bookbridge && go test ./internal/store/ -run "TestDiffPopulatesBookShelves|TestBaselineUsesBookShelves|TestSchemaVersionIsTwo" -v`
Expected: FAIL (ShelvesOf undefined / book_shelves missing / version 1).

- [ ] **Step 3: Rewrite the migration to be stepwise and add book_shelves**

In `internal/store/store.go`, change `const schemaVersion = 1` to `const schemaVersion = 2` and replace the entire `migrate()` method with:
```go
func (s *Store) migrate() error {
	var ver int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&ver); err != nil {
		return err
	}
	if ver > schemaVersion {
		return fmt.Errorf("db schema v%d is newer than supported v%d; upgrade BookBridge", ver, schemaVersion)
	}
	migrations := []string{
		// v1
		`CREATE TABLE IF NOT EXISTS books (
  source TEXT NOT NULL, external_id TEXT NOT NULL, title TEXT NOT NULL, author TEXT NOT NULL,
  isbn10 TEXT, year INTEGER, cover_url TEXT, added_at TEXT,
  first_seen_at TEXT NOT NULL DEFAULT (datetime('now')), state TEXT NOT NULL,
  work_id TEXT, chosen_language TEXT, chosen_format TEXT, shelfarr_request_id TEXT,
  attempt_count INTEGER NOT NULL DEFAULT 0, updated_at TEXT NOT NULL DEFAULT (datetime('now')),
  PRIMARY KEY (source, external_id));
CREATE INDEX IF NOT EXISTS idx_books_state ON books(state);
CREATE TABLE IF NOT EXISTS shelf_config (
  shelf TEXT PRIMARY KEY, enabled INTEGER NOT NULL DEFAULT 1, baselined_at TEXT, format TEXT, language TEXT);`,
		// v2
		`CREATE TABLE IF NOT EXISTS book_shelves (
  source TEXT NOT NULL, external_id TEXT NOT NULL, shelf TEXT NOT NULL,
  PRIMARY KEY (source, external_id, shelf));
CREATE INDEX IF NOT EXISTS idx_book_shelves_shelf ON book_shelves(shelf);`,
	}
	for i := ver; i < schemaVersion; i++ {
		if _, err := s.db.Exec(migrations[i]); err != nil {
			return fmt.Errorf("migration to v%d: %w", i+1, err)
		}
	}
	if ver < schemaVersion {
		if _, err := s.db.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Update Diff and BaselineShelf to use book_shelves**

In `internal/store/books.go`, replace the `Diff` insert loop body and `BaselineShelf` with:
```go
func (s *Store) Diff(ctx context.Context, books []sources.Book) ([]sources.Book, error) {
	var out []sources.Book
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, b := range books {
		// always (re)assert shelf membership, even for known books
		for _, sh := range b.Shelves {
			if _, err := tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO book_shelves(source,external_id,shelf) VALUES(?,?,?)`,
				b.Source, b.ExternalID, sh); err != nil {
				return nil, err
			}
		}
		var exists int
		if err := tx.QueryRowContext(ctx,
			`SELECT 1 FROM books WHERE source=? AND external_id=?`, b.Source, b.ExternalID).Scan(&exists); err == nil {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO books(source,external_id,title,author,isbn10,year,cover_url,added_at,state)
			 VALUES(?,?,?,?,?,?,?,?, 'new')`,
			b.Source, b.ExternalID, b.Title, b.Author, b.ISBN10, b.Year, b.CoverURL,
			b.AddedAt.Format("2006-01-02T15:04:05Z07:00")); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// BaselineShelf marks current 'new' books that belong to `shelf` as 'baseline'.
func (s *Store) BaselineShelf(ctx context.Context, shelf string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE books SET state='baseline', updated_at=datetime('now')
		 WHERE state='new' AND EXISTS (
		   SELECT 1 FROM book_shelves bs
		   WHERE bs.source=books.source AND bs.external_id=books.external_id AND bs.shelf=?)`, shelf)
	return err
}

// ShelvesOf returns the shelves a book belongs to.
func (s *Store) ShelvesOf(ctx context.Context, source, externalID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT shelf FROM book_shelves WHERE source=? AND external_id=?`, source, externalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var sh string
		if err := rows.Scan(&sh); err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}
```

Remove the now-obsolete note about `chosen_format` carrying shelves (it no longer does).

- [ ] **Step 5: Run the store tests — must pass**

Run (Bash): `export PATH="/c/Program Files/Go/bin:$PATH" && cd /c/source/repo/shelfarr-bookbridge && go test ./internal/store/ -v`
Expected: PASS (including the existing Plan-1 store tests).

- [ ] **Step 6: Run the full suite + commit**

Run: `go test ./...` → all green.
```bash
git add internal/store/
git commit -m "feat(store): book_shelves table + stepwise migrations (schema v2)"
```

### Task 2: HTML list parser

**Files:**
- Create: `internal/sources/goodreads/html.go`
- Test: `internal/sources/goodreads/html_test.go`

**Interfaces:**
- Consumes: `sources.Book`, goquery.
- Produces: `parseHTMLList(data []byte, shelf string) (books []sources.Book, signedOut bool, err error)`. `signedOut=true` when the page is the sign-in wall. `book_id` = leading digits of the `/book/show/<id>...` href.

- [ ] **Step 1: Write the failing test**

Create `internal/sources/goodreads/html_test.go`:
```go
package goodreads

import "testing"

const sampleHTML = `<html><head><title>Rafa's books</title></head><body>
<table><tbody id="booksBody">
<tr id="review_1">
 <td class="field title"><div class="value"><a href="/book/show/12345.El_Nombre_del_Viento" title="El Nombre del Viento">El Nombre del Viento</a></div></td>
 <td class="field author"><div class="value"><a href="/author/show/1.x">Rothfuss, Patrick</a></div></td>
 <td class="field isbn"><div class="value">8401352835</div></td>
</tr>
<tr id="review_2">
 <td class="field title"><div class="value"><a href="/book/show/67890.The_Wise_Mans_Fear" title="The Wise Man's Fear">The Wise Man&#39;s Fear</a></div></td>
 <td class="field author"><div class="value"><a href="/author/show/1.x">Rothfuss, Patrick</a></div></td>
 <td class="field isbn"><div class="value"></div></td>
</tr>
</tbody></table></body></html>`

const signinHTML = `<html><head><title>Sign in</title></head><body><div id="choices">sign in</div></body></html>`

func TestParseHTMLList(t *testing.T) {
	books, signedOut, err := parseHTMLList([]byte(sampleHTML), "to-read")
	if err != nil {
		t.Fatal(err)
	}
	if signedOut {
		t.Fatal("should not be signed out")
	}
	if len(books) != 2 {
		t.Fatalf("want 2 books, got %d", len(books))
	}
	if books[0].ExternalID != "12345" || books[0].Source != "goodreads" {
		t.Fatalf("bad id: %+v", books[0])
	}
	if books[0].Title != "El Nombre del Viento" {
		t.Fatalf("bad title: %q", books[0].Title)
	}
	if books[1].ISBN10 != "" {
		t.Fatalf("empty isbn should stay empty, got %q", books[1].ISBN10)
	}
	if books[0].Shelves[0] != "to-read" {
		t.Fatalf("shelf not tagged: %v", books[0].Shelves)
	}
}

func TestParseHTMLListDetectsSignIn(t *testing.T) {
	_, signedOut, err := parseHTMLList([]byte(signinHTML), "to-read")
	if err != nil {
		t.Fatal(err)
	}
	if !signedOut {
		t.Fatal("expected signedOut=true for sign-in page")
	}
}
```

- [ ] **Step 2: Run it — must fail**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && cd /c/source/repo/shelfarr-bookbridge && go test ./internal/sources/goodreads/ -run TestParseHTMLList -v`
Expected: FAIL (parseHTMLList undefined).

- [ ] **Step 3: Implement**

Create `internal/sources/goodreads/html.go`:
```go
package goodreads

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

var bookIDRe = regexp.MustCompile(`/book/show/(\d+)`)

// parseHTMLList parses a Goodreads review/list table page. signedOut is true
// when the page is the login wall (so the caller can surface ErrCookieExpired).
func parseHTMLList(data []byte, shelf string) ([]sources.Book, bool, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(data))
	if err != nil {
		return nil, false, err
	}
	if title := strings.ToLower(strings.TrimSpace(doc.Find("head title").Text())); strings.Contains(title, "sign in") {
		return nil, true, nil
	}
	if doc.Find("#choices, #signedOutBanner, #userSignInForm").Length() > 0 {
		return nil, true, nil
	}
	var out []sources.Book
	doc.Find("tbody#booksBody > tr").Each(func(_ int, row *goquery.Selection) {
		href, _ := row.Find("td.field.title div.value a").Attr("href")
		m := bookIDRe.FindStringSubmatch(href)
		if m == nil {
			return // malformed row — skip
		}
		title := strings.TrimSpace(row.Find("td.field.title div.value a").Text())
		author := strings.TrimSpace(row.Find("td.field.author div.value a").Text())
		isbn := strings.TrimSpace(row.Find("td.field.isbn div.value").Text())
		out = append(out, sources.Book{
			Source:     "goodreads",
			ExternalID: m[1],
			Title:      title,
			Author:     author,
			ISBN10:     isbn,
			Shelves:    []string{shelf},
		})
	})
	return out, false, nil
}
```

- [ ] **Step 4: Run it — must pass**

Run: `go test ./internal/sources/goodreads/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/sources/goodreads/html.go internal/sources/goodreads/html_test.go go.mod go.sum
git commit -m "feat(goodreads): HTML review/list parser + sign-in detection"
```

### Task 3: Cookie HTML source (fetch + pagination)

**Files:**
- Create: `internal/sources/goodreads/htmlsource.go`
- Test: `internal/sources/goodreads/htmlsource_test.go`

**Interfaces:**
- Consumes: `config.SecretString`, `parseHTMLList`, `sources.Book`.
- Produces: `var ErrCookieExpired = errors.New(...)`; `goodreads.NewHTMLSource(userID string, cookie config.SecretString, base string, hc *http.Client) *HTMLSource` implementing `sources.Source`; paginates `per_page=100&print=true&page=N`, stops on an empty page, returns `ErrCookieExpired` on the sign-in wall.

- [ ] **Step 1: Write the failing test**

Create `internal/sources/goodreads/htmlsource_test.go`:
```go
package goodreads

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
)

func TestHTMLSourcePaginatesUntilEmpty(t *testing.T) {
	var gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		page := r.URL.Query().Get("page")
		switch page {
		case "1":
			w.Write([]byte(sampleHTML)) // 2 books
		default:
			w.Write([]byte(`<html><head><title>x</title></head><body><tbody id="booksBody"></tbody><div class="greyText nocontent stacked">no books</div></body></html>`))
		}
	}))
	defer srv.Close()
	s := NewHTMLSource("42", config.SecretString("sess=abc"), srv.URL, srv.Client())
	books, err := s.Fetch(context.Background(), []string{"to-read"})
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 2 {
		t.Fatalf("want 2, got %d", len(books))
	}
	if !strings.Contains(gotCookie, "sess=abc") {
		t.Fatalf("cookie not sent: %q", gotCookie)
	}
}

func TestHTMLSourceCookieExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, signinHTML)
	}))
	defer srv.Close()
	s := NewHTMLSource("42", config.SecretString("sess=stale"), srv.URL, srv.Client())
	_, err := s.Fetch(context.Background(), []string{"to-read"})
	if !errors.Is(err, ErrCookieExpired) {
		t.Fatalf("want ErrCookieExpired, got %v", err)
	}
}
```

- [ ] **Step 2: Run it — must fail**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && cd /c/source/repo/shelfarr-bookbridge && go test ./internal/sources/goodreads/ -run TestHTMLSource -v`
Expected: FAIL (NewHTMLSource undefined).

- [ ] **Step 3: Implement**

Create `internal/sources/goodreads/htmlsource.go`:
```go
package goodreads

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
)

var ErrCookieExpired = errors.New("goodreads session cookie expired or invalid — re-grab it from your browser DevTools")

const maxHTMLPages = 50 // safety bound (50*100 = 5000 books)

type HTMLSource struct {
	userID string
	cookie config.SecretString
	base   string
	hc     *http.Client
}

func NewHTMLSource(userID string, cookie config.SecretString, base string, hc *http.Client) *HTMLSource {
	if base == "" {
		base = "https://www.goodreads.com"
	}
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &HTMLSource{userID: userID, cookie: cookie, base: base, hc: hc}
}

func (s *HTMLSource) Fetch(ctx context.Context, shelves []string) ([]sources.Book, error) {
	var all []sources.Book
	for _, shelf := range shelves {
		seen := map[string]struct{}{}
		for page := 1; page <= maxHTMLPages; page++ {
			q := url.Values{
				"shelf":    {shelf},
				"per_page": {"100"},
				"page":     {strconv.Itoa(page)},
				"print":    {"true"},
			}
			u := fmt.Sprintf("%s/review/list/%s?%s", s.base, url.PathEscape(s.userID), q.Encode())
			req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
			if err != nil {
				return nil, err
			}
			req.Header.Set("User-Agent", "shelfarr-bookbridge/0.1 (+self-hosted)")
			req.Header.Set("Cookie", s.cookie.Reveal())
			resp, err := s.hc.Do(req)
			if err != nil {
				return nil, fmt.Errorf("goodreads html fetch %q p%d: %w", shelf, page, err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != 200 {
				return nil, fmt.Errorf("goodreads html fetch %q p%d: HTTP %d", shelf, page, resp.StatusCode)
			}
			books, signedOut, err := parseHTMLList(body, shelf)
			if err != nil {
				return nil, err
			}
			if signedOut {
				return nil, ErrCookieExpired
			}
			if len(books) == 0 {
				break // sentinel / past the last page
			}
			for _, b := range books {
				if _, dup := seen[b.ExternalID]; dup {
					continue
				}
				seen[b.ExternalID] = struct{}{}
				all = append(all, b)
			}
			time.Sleep(jitterDelay()) // politeness between pages
		}
	}
	return all, nil
}

func jitterDelay() time.Duration { return 1500 * time.Millisecond }
```

- [ ] **Step 4: Run it — must pass**

Run: `go test ./internal/sources/goodreads/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/sources/goodreads/htmlsource.go internal/sources/goodreads/htmlsource_test.go
git commit -m "feat(goodreads): cookie-authenticated HTML source with pagination"
```

### Task 4: Composite source selector + RSS↔HTML contract test

**Files:**
- Modify: `internal/sources/goodreads/source.go`
- Test: `internal/sources/goodreads/contract_test.go`

**Interfaces:**
- Consumes: `RSSSource`, `HTMLSource`, `config.SecretString`.
- Produces: `goodreads.NewSource(userID string, feedKey, cookie config.SecretString, base string, hc *http.Client) sources.Source` — returns the HTML source when a cookie is set, else the RSS source.

- [ ] **Step 1: Write the failing tests**

Create `internal/sources/goodreads/contract_test.go`:
```go
package goodreads

import (
	"sort"
	"testing"
)

// Invariant: the RSS and HTML parsers must emit identical ExternalID sets for
// the same shelf, or crossing the 100-cap re-requests every book.
func TestRSSAndHTMLExternalIDsMatch(t *testing.T) {
	rss, err := parseRSS([]byte(sampleRSS), "to-read")
	if err != nil {
		t.Fatal(err)
	}
	html, _, err := parseHTMLList([]byte(sampleHTML), "to-read")
	if err != nil {
		t.Fatal(err)
	}
	ids := func(bs []idHaver) []string {
		var out []string
		for _, b := range bs {
			out = append(out, b.id())
		}
		sort.Strings(out)
		return out
	}
	_ = ids
	var rssIDs, htmlIDs []string
	for _, b := range rss {
		rssIDs = append(rssIDs, b.ExternalID)
	}
	for _, b := range html {
		htmlIDs = append(htmlIDs, b.ExternalID)
	}
	sort.Strings(rssIDs)
	sort.Strings(htmlIDs)
	if len(rssIDs) != len(htmlIDs) {
		t.Fatalf("id count differs: rss=%v html=%v", rssIDs, htmlIDs)
	}
	for i := range rssIDs {
		if rssIDs[i] != htmlIDs[i] {
			t.Fatalf("id mismatch at %d: rss=%q html=%q", i, rssIDs[i], htmlIDs[i])
		}
	}
}

type idHaver interface{ id() string }

func TestNewSourceSelectsByCookie(t *testing.T) {
	withCookie := NewSource("42", "", "sess=abc", "", nil)
	if _, ok := withCookie.(*HTMLSource); !ok {
		t.Fatalf("cookie set should give *HTMLSource, got %T", withCookie)
	}
	noCookie := NewSource("42", "feedkey", "", "", nil)
	if _, ok := noCookie.(*RSSSource); !ok {
		t.Fatalf("no cookie should give *RSSSource, got %T", noCookie)
	}
}
```

> Note: the `sampleRSS` (Task 9, Plan 1) and `sampleHTML` (Task 2) fixtures both use book_ids `12345` and `67890` — that is what makes the equivalence test meaningful. If they drift, fix the fixtures, not the test.

- [ ] **Step 2: Run it — must fail**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && cd /c/source/repo/shelfarr-bookbridge && go test ./internal/sources/goodreads/ -run "TestRSSAndHTML|TestNewSource" -v`
Expected: FAIL (NewSource undefined). If the ID-equivalence test fails, align the two fixtures to share ids `12345`/`67890`.

- [ ] **Step 3: Implement NewSource**

Append to `internal/sources/goodreads/source.go`:
```go
import "github.com/JanitorHead/shelfarr-bookbridge/internal/config"

// NewSource selects the read strategy: cookie set -> authenticated HTML (any
// size); otherwise RSS (public or private-via-feedKey, capped at 100).
func NewSource(userID string, feedKey, cookie config.SecretString, base string, hc *httpClient) sources.Source {
	if cookie.Reveal() != "" {
		return NewHTMLSource(userID, cookie, base, hc)
	}
	return NewRSSSource(userID, feedKey, base, hc)
}
```

> If `source.go` already imports `net/http` and `config`, do not duplicate the import lines — add `config` to the existing import block and use `*http.Client` as the last parameter type (replace the placeholder `*httpClient` with `*http.Client`).

- [ ] **Step 4: Run it — must pass**

Run: `go test ./internal/sources/goodreads/ -v`
Expected: PASS. (Confirm `sampleRSS` and `sampleHTML` share book_ids `12345`/`67890`; adjust the fixtures if the equivalence test fails.)

- [ ] **Step 5: Commit**

```bash
git add internal/sources/goodreads/source.go internal/sources/goodreads/contract_test.go internal/sources/goodreads/rss_test.go
git commit -m "feat(goodreads): composite source selector + RSS/HTML ID-equivalence contract test"
```

### Task 5: Wire the composite source into the CLI

**Files:**
- Modify: `internal/config/config.go`
- Modify: `cmd/bookbridge/main.go`
- Test: `cmd/bookbridge/cookie_test.go`

**Interfaces:**
- Consumes: `goodreads.NewSource`, `Config.GoodreadsCookie`.
- Produces: `Config.GoodreadsCookie SecretString` (from `GOODREADS_COOKIE`); `run` builds the source via `goodreads.NewSource(userID, feedKey, cookie, base, nil)`.

- [ ] **Step 1: Write the failing test**

Create `cmd/bookbridge/cookie_test.go`:
```go
package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSyncUsesCookieHTMLWhenCookieSet(t *testing.T) {
	var hitHTML bool
	gr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/review/list/") {
			hitHTML = true
			if r.URL.Query().Get("page") == "1" {
				w.Write([]byte(`<html><head><title>x</title></head><body><tbody id="booksBody"><tr><td class="field title"><div class="value"><a href="/book/show/12345.X">Dune</a></div></td><td class="field author"><div class="value"><a href="/author/show/1">Frank Herbert</a></div></td><td class="field isbn"><div class="value"></div></td></tr></tbody></body></html>`))
			} else {
				w.Write([]byte(`<html><head><title>x</title></head><body><tbody id="booksBody"></tbody></body></html>`))
			}
			return
		}
		w.Write([]byte(`{"results":[{"work_id":"ol:1","title":"Dune","author":"Frank Herbert","confidence":90}]}`))
	}))
	defer gr.Close()
	env := map[string]string{
		"SHELFARR_URL": gr.URL, "SHELFARR_TOKEN": "shf_t",
		"GOODREADS_USER_ID": "42", "SHELVES": "to-read",
		"GOODREADS_COOKIE": "sess=abc", "GOODREADS_BASE": gr.URL,
		"BB_DB": filepath.Join(t.TempDir(), "bb.db"),
	}
	var out strings.Builder
	code := run([]string{"sync", "--dry-run"}, func(k string) string { return env[k] }, &out)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if !hitHTML {
		t.Fatal("expected the cookie-HTML route to be used")
	}
	if !strings.Contains(out.String(), "new=1") {
		t.Fatalf("expected new=1, got %s", out.String())
	}
}
```

- [ ] **Step 2: Run it — must fail**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && cd /c/source/repo/shelfarr-bookbridge && go test ./cmd/bookbridge/ -run TestRunSyncUsesCookie -v`
Expected: FAIL (GoodreadsCookie / source selection not wired).

- [ ] **Step 3: Add the config field**

In `internal/config/config.go`, add to the `Config` struct (after `GoodreadsFeedKey`):
```go
	GoodreadsCookie SecretString
```
and in `loadFrom`, after the `GoodreadsFeedKey:` line:
```go
		GoodreadsCookie: SecretString(get("GOODREADS_COOKIE")),
```

- [ ] **Step 4: Switch the CLI to the composite source**

In `cmd/bookbridge/main.go`, replace the line that builds `src`:
```go
	src := goodreads.NewRSSSource(cfg.GoodreadsUserID, cfg.GoodreadsFeedKey, getenv("GOODREADS_BASE"), nil)
```
with:
```go
	src := goodreads.NewSource(cfg.GoodreadsUserID, cfg.GoodreadsFeedKey, cfg.GoodreadsCookie, getenv("GOODREADS_BASE"), nil)
```

- [ ] **Step 5: Run it — must pass**

Run: `go test ./... `
Expected: PASS (all packages).

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go cmd/bookbridge/main.go cmd/bookbridge/cookie_test.go
git commit -m "feat(cli): select cookie-HTML source when GOODREADS_COOKIE is set"
```

---

## Self-Review (performed)

- **Coverage:** book_shelves table + stepwise migration removes the Plan-1 `chosen_format` shelf hack (T1); HTML parser with sign-in detection (T2); cookie-authenticated paginating source with `ErrCookieExpired` (T3); composite selector + the critical RSS↔HTML ExternalID-equivalence invariant (T4); CLI wiring so a configured cookie reads his private >100 shelf (T5). After this plan, `bookbridge sync --apply` works end-to-end for the owner's actual situation.
- **Placeholder scan:** none. The one prose caveat (align `sampleRSS`/`sampleHTML` ids) is a fixture instruction, not a code placeholder.
- **Type consistency:** `parseHTMLList(...)(books, signedOut, err)`, `NewHTMLSource`, `NewSource`, `ErrCookieExpired`, `Config.GoodreadsCookie`, `store.ShelvesOf`/`BaselineShelf` are referenced identically across tasks; `NewSource`'s final param is `*http.Client` (the `*httpClient` placeholder in T4 Step 3 is explicitly corrected in the same step's note).

## Deferred to Phase 2b (next plan)

langdetect (lingua-go) wired into request `language`; status reconciliation (batch `GET /requests?status=`) + bounded recheck + `parked` state + 404 handling; single-flight run lock + `Clock` injection; scheduler + `daemon` mode. Phase 2c: secrets-at-rest encryption, transport/egress hardening, Dockerfile + Unraid template.
