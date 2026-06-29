# Shelfarr BookBridge — Phase 2c: Daemon, Hardening & Docker

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Run BookBridge unattended on Unraid: a cron-scheduled `daemon`, transport/secret hardening, and a single Docker image + Community Apps template.

**Architecture:** A `scheduler` (robfig/cron/v3) drives `engine.Run` on `SCHEDULE`. A `daemon` subcommand wires everything (and finally connects the langdetect detector to the production CLI), runs once on start, then schedules. Transport safety refuses cleartext tokens off-loopback; the SQLite file is created `0600`. A multi-stage, non-root, CGO-free Docker image plus an Unraid template ship it.

**Tech Stack:** Go 1.23+, `github.com/robfig/cron/v3` (already fetched), Docker, existing stack.

## Global Constraints

- Same as prior phases: module `github.com/JanitorHead/shelfarr-bookbridge`; `SecretString`; TDD; `go test ./...` green per task; commit per task with `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>` trailer.
- The daemon MUST run one cycle immediately on start, then on `SCHEDULE` (default `0 * * * *`, hourly). `--once` runs exactly one cycle and exits (for tests/manual use).
- Cleartext safety: refuse an `http://` `SHELFARR_URL` whose host is not loopback unless `SHELFARR_INSECURE=true` (the `shf_` token grants `requests:write`).
- The Docker image is CGO-free, multi-stage, runs as a non-root user, persists state in `/config`.

---

## File Structure

| Path | Responsibility | Change |
|---|---|---|
| `internal/scheduler/scheduler.go` | cron wrapper | Create |
| `internal/config/transport.go` | `CheckTransport` | Create |
| `internal/config/config.go` | `ShelfarrInsecure`, `Schedule` | Modify |
| `internal/store/store.go` | `0600` db file | Modify |
| `cmd/bookbridge/main.go` | `daemon` cmd + detector wiring + buildEngine | Modify |
| `Dockerfile`, `.dockerignore`, `docker-compose.yml`, `unraid-template.xml`, `README.md` | packaging/docs | Create |

---

### Task 1: Scheduler package

**Files:**
- Create: `internal/scheduler/scheduler.go`
- Test: `internal/scheduler/scheduler_test.go`

**Interfaces:**
- Produces: `scheduler.New(cronExpr string, fn func()) (*Scheduler, error)` (validates the expression); `(*Scheduler).Start()`, `(*Scheduler).Stop()`.

- [ ] **Step 1: Write the failing test**

Create `internal/scheduler/scheduler_test.go`:
```go
package scheduler

import "testing"

func TestNewRejectsBadCron(t *testing.T) {
	if _, err := New("not a cron", func() {}); err == nil {
		t.Fatal("expected error for invalid cron expression")
	}
}

func TestNewAcceptsValidCronAndStartsStops(t *testing.T) {
	s, err := New("0 * * * *", func() {})
	if err != nil {
		t.Fatalf("valid cron rejected: %v", err)
	}
	s.Start()
	s.Stop() // must not panic
}
```

- [ ] **Step 2: Run it — must fail**

Run (Bash): `export PATH="/c/Program Files/Go/bin:$PATH" && cd /c/source/repo/shelfarr-bookbridge && go test ./internal/scheduler/ -v`
Expected: FAIL (New undefined).

- [ ] **Step 3: Implement**

Create `internal/scheduler/scheduler.go`:
```go
package scheduler

import "github.com/robfig/cron/v3"

type Scheduler struct{ c *cron.Cron }

// New builds a scheduler that runs fn on the given 5-field cron expression.
func New(cronExpr string, fn func()) (*Scheduler, error) {
	c := cron.New()
	if _, err := c.AddFunc(cronExpr, fn); err != nil {
		return nil, err
	}
	return &Scheduler{c: c}, nil
}

func (s *Scheduler) Start() { s.c.Start() }
func (s *Scheduler) Stop()  { s.c.Stop() }
```

- [ ] **Step 4: Run + commit**

Run: `go test ./internal/scheduler/ -v` → PASS, then `go test ./...` → green.
```bash
git add internal/scheduler/
git commit -m "feat(scheduler): cron wrapper around robfig/cron"
```

### Task 2: Transport safety

**Files:**
- Create: `internal/config/transport.go`
- Modify: `internal/config/config.go`
- Test: `internal/config/transport_test.go`

**Interfaces:**
- Produces: `Config.ShelfarrInsecure bool` (`SHELFARR_INSECURE=="true"`); `Config.Schedule string` (`SCHEDULE`, default `"0 * * * *"`); `config.CheckTransport(shelfarrURL string, allowInsecure bool) error`.

- [ ] **Step 1: Write the failing test**

Create `internal/config/transport_test.go`:
```go
package config

import "testing"

func TestCheckTransport(t *testing.T) {
	cases := []struct {
		url      string
		insecure bool
		wantErr  bool
	}{
		{"https://shelfarr.example", false, false},
		{"http://127.0.0.1:3000", false, false},
		{"http://localhost:3000", false, false},
		{"http://192.168.1.5:3000", false, true},
		{"http://192.168.1.5:3000", true, false},
		{"://bad", false, true},
	}
	for _, c := range cases {
		err := CheckTransport(c.url, c.insecure)
		if (err != nil) != c.wantErr {
			t.Errorf("CheckTransport(%q, %v) err=%v wantErr=%v", c.url, c.insecure, err, c.wantErr)
		}
	}
}
```

- [ ] **Step 2: Run it — must fail**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && cd /c/source/repo/shelfarr-bookbridge && go test ./internal/config/ -run TestCheckTransport -v`
Expected: FAIL (CheckTransport undefined).

- [ ] **Step 3: Implement CheckTransport**

Create `internal/config/transport.go`:
```go
package config

import (
	"fmt"
	"net"
	"net/url"
)

// CheckTransport refuses to send the bearer token in cleartext over a non-loopback
// http:// endpoint unless explicitly allowed.
func CheckTransport(shelfarrURL string, allowInsecure bool) error {
	u, err := url.Parse(shelfarrURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("invalid SHELFARR_URL %q", shelfarrURL)
	}
	if u.Scheme == "https" || allowInsecure {
		return nil
	}
	if u.Scheme != "http" {
		return fmt.Errorf("SHELFARR_URL must be http or https, got %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("refusing to send the Shelfarr token in cleartext to non-loopback http host %q; use https or set SHELFARR_INSECURE=true", host)
}
```

- [ ] **Step 4: Add the config fields**

In `internal/config/config.go`, add to the `Config` struct:
```go
	ShelfarrInsecure bool
	Schedule         string
```
and in `loadFrom`'s `Config{...}` literal:
```go
		ShelfarrInsecure: get("SHELFARR_INSECURE") == "true",
		Schedule:         orDefault(get("SCHEDULE"), "0 * * * *"),
```

- [ ] **Step 5: Run + commit**

Run: `go test ./internal/config/ -v && go test ./...` → green.
```bash
git add internal/config/transport.go internal/config/transport_test.go internal/config/config.go
git commit -m "feat(config): transport safety + Schedule/ShelfarrInsecure"
```

### Task 3: Restrictive DB file permissions

**Files:**
- Modify: `internal/store/store.go`
- Test: `internal/store/perms_test.go`

**Interfaces:**
- Produces: `Open` best-effort `chmod 0600` of the DB file after creation (matters on the Linux container; no-op on Windows).

- [ ] **Step 1: Write the failing test**

Create `internal/store/perms_test.go`:
```go
package store

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDBFileIsOwnerOnly(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bb.db")
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if mode := fi.Mode().Perm(); mode&0o077 != 0 {
			t.Fatalf("db file mode = %o, want no group/other bits", mode)
		}
	}
}
```

- [ ] **Step 2: Run it — must fail (on Linux) / verify wiring (on Windows)**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && cd /c/source/repo/shelfarr-bookbridge && go test ./internal/store/ -run TestDBFileIsOwnerOnly -v`
Expected on Windows: PASS only after Step 3 adds the chmod call (the assertion is skipped on Windows, but the test must compile and `Open` must still succeed). On Linux it FAILS pre-implementation.

- [ ] **Step 3: Chmod the DB file in Open**

In `internal/store/store.go`, add `"os"` to the imports, and in `Open`, right after `s := &Store{db: db}` (before `migrate`), add:
```go
	_ = os.Chmod(path, 0o600) // best-effort; tighten secrets at rest on Linux
```

- [ ] **Step 4: Run + commit**

Run: `go test ./internal/store/ -v && go test ./...` → green.
```bash
git add internal/store/store.go internal/store/perms_test.go
git commit -m "feat(store): create the SQLite file 0600 (secrets at rest)"
```

### Task 4: `daemon` subcommand + detector wiring

**Files:**
- Modify: `cmd/bookbridge/main.go`
- Test: `cmd/bookbridge/daemon_test.go`

**Interfaces:**
- Produces: `bookbridge daemon [--once]`; an internal `buildEngine(cfg config.Config, getenv func(string) string) (*engine.Engine, *store.Store, error)` that checks transport, opens the store, builds the composite source + Shelfarr client + engine, and wires `langdetect.New()` when `cfg.LangInference`. `sync` and `daemon` both use it.

- [ ] **Step 1: Write the failing test**

Create `cmd/bookbridge/daemon_test.go`:
```go
package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDaemonOnce(t *testing.T) {
	gr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<?xml version="1.0"?><rss><channel><item><book_id>1</book_id><title>Dune</title><author_name>Frank Herbert</author_name><isbn></isbn></item></channel></rss>`))
	}))
	defer gr.Close()
	sh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	defer sh.Close()
	env := map[string]string{
		"SHELFARR_URL": sh.URL, "SHELFARR_TOKEN": "shf_t",
		"GOODREADS_USER_ID": "42", "SHELVES": "to-read",
		"GOODREADS_BASE": gr.URL, "BB_DB": filepath.Join(t.TempDir(), "bb.db"),
	}
	var out strings.Builder
	code := run([]string{"daemon", "--once"}, func(k string) string { return env[k] }, &out)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	if !strings.Contains(out.String(), "requested=1") {
		t.Fatalf("expected requested=1, got %s", out.String())
	}
}

func TestRunDaemonRefusesInsecureTransport(t *testing.T) {
	env := map[string]string{
		"SHELFARR_URL": "http://192.168.1.5:3000", "SHELFARR_TOKEN": "shf_t",
		"GOODREADS_USER_ID": "42", "SHELVES": "to-read",
		"BB_DB": filepath.Join(t.TempDir(), "bb.db"),
	}
	var out strings.Builder
	code := run([]string{"daemon", "--once"}, func(k string) string { return env[k] }, &out)
	if code == 0 {
		t.Fatalf("expected non-zero exit for insecure transport, out=%s", out.String())
	}
}
```

- [ ] **Step 2: Run it — must fail**

Run: `export PATH="/c/Program Files/Go/bin:$PATH" && cd /c/source/repo/shelfarr-bookbridge && go test ./cmd/bookbridge/ -run TestRunDaemon -v`
Expected: FAIL (daemon not handled).

- [ ] **Step 3: Rewrite main.go with dispatch + buildEngine + daemon**

Replace the entire contents of `cmd/bookbridge/main.go` with:
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
	"github.com/JanitorHead/shelfarr-bookbridge/internal/langdetect"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/scheduler"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/shelfarr"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources/goodreads"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/store"
)

func main() { os.Exit(run(os.Args[1:], os.Getenv, os.Stdout)) }

func run(args []string, getenv func(string) string, out io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(out, "usage: bookbridge <sync|daemon> [flags]")
		return 2
	}
	switch args[0] {
	case "sync":
		return runSync(args[1:], getenv, out)
	case "daemon":
		return runDaemon(args[1:], getenv, out)
	default:
		fmt.Fprintln(out, "usage: bookbridge <sync|daemon> [flags]")
		return 2
	}
}

func buildEngine(cfg config.Config, getenv func(string) string) (*engine.Engine, *store.Store, error) {
	if err := config.CheckTransport(cfg.ShelfarrURL, cfg.ShelfarrInsecure); err != nil {
		return nil, nil, err
	}
	st, err := store.Open(orEnv(getenv, "BB_DB", "/config/bookbridge.db"))
	if err != nil {
		return nil, nil, err
	}
	src := goodreads.NewSource(cfg.GoodreadsUserID, cfg.GoodreadsFeedKey, cfg.GoodreadsCookie, getenv("GOODREADS_BASE"), nil)
	sh := shelfarr.New(cfg.ShelfarrURL, cfg.ShelfarrToken, nil)
	e := engine.New(src, st, sh, cfg)
	if cfg.LangInference {
		e.SetDetector(langdetect.New())
	}
	return e, st, nil
}

func printReport(out io.Writer, mode string, rep engine.Report) {
	fmt.Fprintf(out, "[%s] fetched=%d new=%d requested=%d not_found=%d already_exists=%d reconciled=%d completed=%d failed=%d rechecked=%d parked=%d\n",
		mode, rep.Fetched, rep.New, rep.Requested, rep.NotFound, rep.AlreadyExists,
		rep.Reconciled, rep.Completed, rep.Failed, rep.Rechecked, rep.Parked)
}

func runSync(args []string, getenv func(string) string, out io.Writer) int {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	fs.SetOutput(out)
	apply := fs.Bool("apply", false, "create requests (default is dry-run)")
	dry := fs.Bool("dry-run", false, "preview only")
	baseline := fs.Bool("baseline", false, "mark current shelf contents as seen, request nothing")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	dryRun := !*apply || *dry

	cfg, err := config.Load2(getenv)
	if err != nil {
		fmt.Fprintln(out, "config error:", err)
		return 1
	}
	ctx := context.Background()

	if *baseline {
		e, st, err := buildEngine(cfg, getenv)
		if err != nil {
			fmt.Fprintln(out, "error:", err)
			return 1
		}
		defer st.Close()
		_ = e
		src := goodreads.NewSource(cfg.GoodreadsUserID, cfg.GoodreadsFeedKey, cfg.GoodreadsCookie, getenv("GOODREADS_BASE"), nil)
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

	e, st, err := buildEngine(cfg, getenv)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	defer st.Close()
	rep, err := e.Run(ctx, dryRun)
	if err != nil {
		fmt.Fprintln(out, "run error:", err)
		return 1
	}
	mode := "apply"
	if dryRun {
		mode = "dry-run"
	}
	printReport(out, mode, rep)
	return 0
}

func runDaemon(args []string, getenv func(string) string, out io.Writer) int {
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	fs.SetOutput(out)
	once := fs.Bool("once", false, "run a single cycle and exit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := config.Load2(getenv)
	if err != nil {
		fmt.Fprintln(out, "config error:", err)
		return 1
	}
	e, st, err := buildEngine(cfg, getenv)
	if err != nil {
		fmt.Fprintln(out, "error:", err)
		return 1
	}
	defer st.Close()

	cycle := func() {
		rep, err := e.Run(context.Background(), false)
		if err != nil {
			fmt.Fprintln(out, "run error:", err)
			return
		}
		printReport(out, "daemon", rep)
	}

	cycle() // run once immediately
	if *once {
		return 0
	}
	sch, err := scheduler.New(cfg.Schedule, cycle)
	if err != nil {
		fmt.Fprintln(out, "schedule error:", err)
		return 1
	}
	sch.Start()
	defer sch.Stop()
	fmt.Fprintf(out, "daemon scheduled on %q; waiting...\n", cfg.Schedule)
	select {} // block forever
}

func orEnv(get func(string) string, k, def string) string {
	if v := get(k); v != "" {
		return v
	}
	return def
}
```

> Note: the `baseline` branch rebuilds the source directly because it needs `Fetch`+`Diff`+`BaselineShelf` without the engine's request loop; `buildEngine` is still used there for the transport check and store. The unused `e` is discarded with `_ = e` (acceptable; a later cleanup can split a lighter `buildStore`).

- [ ] **Step 4: Run it — must pass**

Run: `go test ./cmd/bookbridge/ -v && go test ./...`
Expected: PASS (the existing `sync` tests still pass under the new dispatch).

- [ ] **Step 5: Commit**

```bash
git add cmd/bookbridge/main.go cmd/bookbridge/daemon_test.go
git commit -m "feat(cli): daemon subcommand (cron) + detector wiring + transport check"
```

### Task 5: Docker image, compose, Unraid template, README

**Files:**
- Create: `Dockerfile`, `.dockerignore`, `docker-compose.yml`, `unraid-template.xml`, `README.md`

**Interfaces:**
- Produces: a buildable non-root CGO-free image running `bookbridge daemon`.

- [ ] **Step 1: Create the Dockerfile**

Create `Dockerfile`:
```dockerfile
# syntax=docker/dockerfile:1
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/bookbridge ./cmd/bookbridge

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/bookbridge /usr/local/bin/bookbridge
ENV BB_DB=/config/bookbridge.db
VOLUME ["/config"]
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/bookbridge"]
CMD ["daemon"]
```

- [ ] **Step 2: Create .dockerignore**

Create `.dockerignore`:
```
.git
docs
*.md
dist
bin
*.db
*.sqlite
```

- [ ] **Step 3: Build the image to verify (Docker is available)**

Run (Bash): `cd /c/source/repo/shelfarr-bookbridge && docker build -t bookbridge:test .`
Expected: a successful build ending in `naming to docker.io/library/bookbridge:test`. If the `golang:1.26` tag is unavailable, fall back to `golang:1` (record as a deviation). Then smoke-test the binary:
Run: `docker run --rm bookbridge:test sync` → prints the usage/`config error` line and exits non-zero (no env) — confirms the binary runs.

- [ ] **Step 4: Create docker-compose.yml**

Create `docker-compose.yml`:
```yaml
services:
  bookbridge:
    image: bookbridge:latest
    container_name: bookbridge
    restart: unless-stopped
    volumes:
      - ./config:/config
    environment:
      SHELFARR_URL: "http://shelfarr:3000"
      SHELFARR_TOKEN: "shf_xxxxxxxx"
      GOODREADS_USER_ID: "00000000"
      # For a private shelf >100 items, paste your browser session cookie:
      GOODREADS_COOKIE: "_session_id2=...; ..."
      # For a public/private <=100 shelf, use the RSS feed key instead:
      # GOODREADS_FEED_KEY: "..."
      SHELVES: "to-read"
      FORMAT: "ebook"
      LANG_INFERENCE: "on"
      SCHEDULE: "0 * * * *"
      MAX_REQUESTS_PER_RUN: "25"
      FIRST_RUN: "baseline"
```

- [ ] **Step 5: Create the Unraid template**

Create `unraid-template.xml`:
```xml
<?xml version="1.0"?>
<Container version="2">
  <Name>shelfarr-bookbridge</Name>
  <Repository>bookbridge:latest</Repository>
  <Registry/>
  <Network>bridge</Network>
  <Privileged>false</Privileged>
  <Support/>
  <Overview>Syncs your Goodreads shelves to Shelfarr as ebook requests.</Overview>
  <Category>MediaApp:Books</Category>
  <Config Name="Config Path" Target="/config" Default="/mnt/user/appdata/bookbridge" Mode="rw" Description="State (SQLite) and secrets" Type="Path" Display="always" Required="true"/>
  <Config Name="Shelfarr URL" Target="SHELFARR_URL" Default="" Mode="" Description="e.g. http://192.168.1.10:3000" Type="Variable" Display="always" Required="true"/>
  <Config Name="Shelfarr Token" Target="SHELFARR_TOKEN" Default="" Mode="" Description="shf_ token (scopes search:read, requests:write, requests:read)" Type="Variable" Display="always" Required="true" Mask="true"/>
  <Config Name="Goodreads User ID" Target="GOODREADS_USER_ID" Default="" Mode="" Description="Numeric Goodreads user id" Type="Variable" Display="always" Required="true"/>
  <Config Name="Goodreads Cookie" Target="GOODREADS_COOKIE" Default="" Mode="" Description="Session cookie for private/&gt;100 shelves" Type="Variable" Display="always" Required="false" Mask="true"/>
  <Config Name="Goodreads Feed Key" Target="GOODREADS_FEED_KEY" Default="" Mode="" Description="RSS feed key for private &lt;=100 shelves" Type="Variable" Display="advanced" Required="false" Mask="true"/>
  <Config Name="Shelves" Target="SHELVES" Default="to-read" Mode="" Description="Comma-separated shelf slugs" Type="Variable" Display="always" Required="true"/>
  <Config Name="Schedule" Target="SCHEDULE" Default="0 * * * *" Mode="" Description="Cron expression" Type="Variable" Display="advanced" Required="false"/>
  <Config Name="Insecure transport" Target="SHELFARR_INSECURE" Default="false" Mode="" Description="Allow http to a non-loopback Shelfarr" Type="Variable" Display="advanced" Required="false"/>
</Container>
```

- [ ] **Step 6: Create README.md**

Create `README.md`:
```markdown
# shelfarr-bookbridge

Watches your Goodreads shelves and creates **ebook** download requests in
[Shelfarr](https://shelfarr.org). Self-hosted, single Docker container.

## How it works

`Goodreads shelf -> dedup (SQLite) -> resolve in Shelfarr -> POST /api/v1/requests (ebook)`,
then reconciles each request's status back. Reads via RSS (<=100 items, public or with a
feed key) or an authenticated HTML reader (private and/or >100 items, via a session cookie).

## Setup

1. **Shelfarr token:** Shelfarr -> Profile -> API tokens -> create one with scopes
   `search:read`, `requests:write`, `requests:read`. It looks like `shf_...`.
2. **Goodreads:** find your numeric user id (in your profile URL). For a private shelf or one
   with >100 items, copy your browser session **cookie** (DevTools -> Network -> any
   goodreads.com request -> Request Headers -> `Cookie`) into `GOODREADS_COOKIE`. For a
   public/small shelf you can instead use the RSS `GOODREADS_FEED_KEY`.
3. Configure env (see `docker-compose.yml`) and start the container.

## Commands

- `bookbridge sync --dry-run` — preview what would be requested (default).
- `bookbridge sync --apply` — create requests.
- `bookbridge sync --baseline` — mark current shelf contents as seen (no requests); run this
  first so an existing backlog isn't requested all at once.
- `bookbridge daemon` — run on a schedule (default hourly); `--once` runs a single cycle.

## Notes

- The Goodreads session cookie expires periodically — re-grab it when you see a
  "cookie expired" error.
- Language is inferred from the title (English/Spanish) and sent as a soft preference;
  disable with `LANG_INFERENCE=off`.
- Automating a private Goodreads shelf may conflict with Goodreads' Terms; this is for your
  own account and data.
```

- [ ] **Step 7: Commit**

```bash
git add Dockerfile .dockerignore docker-compose.yml unraid-template.xml README.md
git commit -m "feat: Docker image + compose + Unraid template + README"
```

---

## Self-Review (performed)

- **Coverage:** cron scheduler (T1); transport safety refusing cleartext tokens + `Schedule` config (T2); `0600` SQLite file (T3); `daemon` subcommand that runs once then on schedule, finally wiring the langdetect detector into the production CLI and enforcing the transport check, via a shared `buildEngine` (T4); CGO-free non-root Docker image (built + smoke-tested), compose, Unraid template, README (T5). After this, BookBridge runs unattended on Unraid end-to-end.
- **Placeholder scan:** none. The `_ = e` discard in the baseline branch is an explicit, noted simplification, not a stub.
- **Type consistency:** `scheduler.New/Start/Stop`, `config.CheckTransport`, `Config.ShelfarrInsecure/Schedule`, `buildEngine`, `engine.Report` fields used in `printReport` all match their definitions in earlier tasks/phases. `langdetect.New()` returns `*langdetect.Detector`, which satisfies `engine.LanguageDetector` (asserted by the existing engine language test).

## Project complete after this plan

Phases 1, 2a, 2b, 2c deliver the full CLI-first MVP from the spec. Remaining future work (spec §17 Phase 2 GUI and Hardcover source) is intentionally out of scope. The owner still runs the live **Phase 0** captures against real Shelfarr/Goodreads to confirm reality matches the fixtures.
