# Shelfarr BookBridge — Design

- **Date:** 2026-06-29
- **Status:** Approved design (pending user review of this spec)
- **Owner:** Rafa

## 1. Problem & Goal

Rafa tracks books to read in **Goodreads** (primary) and has an export in **Hardcover**
(fallback). His downloader is **Shelfarr** (a "Jellyseerr for books": searches
Prowlarr/Jackett/Newznab + direct Anna's Archive / Z-Library / LibriVox, downloads via a
torrent/usenet client, delivers to Audiobookshelf).

Today nothing connects his reading lists to Shelfarr. **Bookotter** exists but it reads
Hardcover only, has no Goodreads support, and downloads through its *own* Prowlarr +
qBittorrent + Kindle-SSH pipeline — it cannot hook into Shelfarr.

**Goal:** a self-hosted service that watches Rafa's Goodreads shelves and automatically
creates ebook download requests in Shelfarr, so new "want to read" books get fetched
without manual work.

The enabling fact: **Shelfarr exposes a REST API** under `/api/v1` with scoped per-user
tokens, explicitly built for "search and request automation":

```
GET  /api/v1/search?q=<isbn|title author>   → resolve a book (OpenLibrary-backed)
POST /api/v1/requests                         → create a download request   ← target
GET  /api/v1/requests                         → list pending/active
DELETE /api/v1/requests/:id                   → cancel
```

So the solution is a thin, deterministic **bridge**: read Goodreads → resolve each new
book in Shelfarr → request it as ebook. "Bookotter's idea, retargeted at Shelfarr's API."

## 2. Non-Goals (YAGNI)

- No downloading/indexer logic of our own — Shelfarr owns all of that.
- No push notifications (Discord/Telegram). Status surfaces in the GUI only.
- No Kindle/e-reader delivery (Audiobookshelf is Shelfarr's job).
- No two-way sync (we never write back to Goodreads/Hardcover).
- No quiet-hours, no complex quality profiles, no LLM in the runtime.

## 3. Core Principle: Determinism

This is a hard tenet of the design.

- **Build/setup time — non-deterministic, one-off (AI + human):** capture a Goodreads
  HAR while logged in and run the `cli-printing-press` factory to generate the
  `goodreads-pp-cli`. This is the *only* AI-assisted, non-reproducible step.
- **Runtime — 100% deterministic:** a Go daemon. No LLM, no opaque fuzzy AI. Rule-based,
  explainable matching; fixed logic; reproducible given the same inputs.

## 4. Decisions Log (resolved during brainstorming)

| Topic | Decision | Rationale |
|---|---|---|
| Architecture | **A — thin custom bridge** to Shelfarr's API | Simplest, robust, no fragile deps; Shelfarr does the hard part. Bookotter fork rejected (too much foreign code/maintenance). |
| Primary source | **Goodreads** (Hardcover = fallback plugin) | Goodreads is Rafa's living list. |
| Goodreads read mode | **Default public/RSS; configurable private/cookie** | RSS has no cookie → zero re-auth, max determinism. Private mode adds a session cookie when needed. |
| Shelves | **Multiple, user-selectable**, with per-shelf format mapping | Not just `to-read`. |
| Format | **ebook** (per-shelf override possible, e.g. an audio shelf → audiobook) | Stated preference. |
| First run | **Baseline by default** (mark existing as seen, act only on new); `backfill` opt-in | Avoid flooding Shelfarr with hundreds of requests. |
| Matching | **Deterministic cascade** (ISBN → normalized title+author); bias to "not found" over wrong match | A wrong download is worse than a manual review. |
| Notifications | **None** (status banner in GUI instead) | Stated preference. |
| GUI | **Simple embedded web UI** to control everything | Stated preference; also where status appears. |
| Packaging | **One self-contained Docker** with an internal scheduler | "Todo encaja mejor en 1 docker"; nothing Unraid-exclusive. Ship an Unraid Community Apps template for convenience. |
| Bridge ↔ CLI | Bridge **invokes `goodreads-pp-cli --json` as a subprocess** | Deterministic and simple; MCP server stays an optional interactive artifact, not in the hot path. |
| PrintingPress | **Generate the Goodreads CLI with the factory now**; contribute it back to `printing-press-library` afterwards | Offloads the fragile reverse-engineered scraping to a maintained, shareable tool. |

### Rejected alternatives
- **RapidAPI Goodreads wrappers** (goodreads12, GoodreadsBooks, etc.): structurally
  unable to read a *private* shelf (they carry no user session → public profiles only),
  and redundant for a *public* profile (the free RSS feed already does it). Strictly
  dominated; adds cost + a third-party dependency + rate limits.
- **Bookotter fork**: no native Goodreads, carries Kindle-SSH baggage, more foreign code
  to gut and maintain.

## 5. Architecture Overview

Single Go binary (one Docker image) containing the daemon, the embedded web GUI, and the
generated `goodreads-pp-cli` binary. SQLite for state. Internal cron scheduler.

```
            ┌──────────────────────── 1 Docker container ───────────────────────────┐
            │                                                                         │
 Goodreads  │  goodreads-pp-cli ──JSON──▶ engine ──┐                                  │
 (RSS pub / │   (RSS or cookie mode)               │   resolver ──▶ shelfarr client   │──▶ Shelfarr
  cookie)   │                                      ▼      (ISBN→title)   (/api/v1)     │     /api/v1
            │  hardcover source ──JSON──▶  store.Diff (SQLite, dedupe)                 │
 Hardcover  │   (GraphQL, fallback)                │                                   │
 (token)    │                                      ▼                                   │
            │                              scheduler (cron) ──▶ engine.Run()           │
            │                              web GUI (go:embed) ◀── reads/controls store │
            │                              volume: /config (SQLite db + session)       │
            └─────────────────────────────────────────────────────────────────────────┘
```

### Engine loop (deterministic)

```
tick (or manual "Sync now") → engine.Run(dryRun):
  for each enabled source:
      books += source.Fetch(enabledShelves)        // goodreads-pp-cli --json / Hardcover GraphQL
  new = store.Diff(books)                            // dedupe by (source, external_id)
  new = filter(new, not ignored, baseline rule)
  for b in new[:MAX_REQUESTS_PER_RUN]:
      work = resolver.Resolve(b, shelfarr)           // ISBN13 → ISBN10 → title+author
      if work and not dryRun:
          shelfarr.CreateRequest(work, format(b.shelf)); store.MarkRequested(b)
      elif work and dryRun:
          store.Preview(b, work)
      else:
          store.MarkNotFound(b)                       // → GUI manual-review queue
  reattempt store.NotFound(olderThan: RECHECK_INTERVAL)   // slow cadence, indexers change
  store.RecordHistory(run summary)
```

## 6. Components (each independently testable)

| Module | Responsibility | Key interface | Depends on |
|---|---|---|---|
| `sources/` | Fetch normalized books from a provider | `Source.Fetch(ctx, shelves) ([]Book, error)` | goodreads-pp-cli (subprocess), Hardcover GraphQL |
| `store/` | Persist state: items, status, history, ignore list, settings | CRUD + `Diff([]Book) []Book` | SQLite |
| `shelfarr/` | Typed client for Shelfarr `/api/v1` | `Search(q) []Work`, `CreateRequest(work, fmt) error` | HTTP |
| `resolver/` | Deterministic Book → Shelfarr Work matching | `Resolve(Book, ShelfarrClient) (*Work, MatchReason)` | shelfarr |
| `engine/` | Orchestrate fetch→diff→resolve→request→record; rate limit, retry, dry-run | `Run(ctx, opts) RunReport` | all above |
| `scheduler/` | Cron-driven invocation of the engine | `Start(cronExpr, engine)` | engine |
| `web/` | Embedded GUI + HTTP handlers (config, manual triggers, review) | net/http + `go:embed` assets | store, engine |
| `config/` | Load + merge env vars and persisted settings | `Load() Config` | store |
| `cmd/bookbridge` | main: wire everything, start scheduler + web | — | all |

`Book` (normalized): `{Source, ExternalID, Title, Author, ISBN10, ISBN13, Shelf, AddedAt}`.

The generated **`goodreads-pp-cli`** lives in its own repo/dir, its binary vendored into
the image at build time. It is not a Go-library dependency of the bridge — the boundary is
the `--json` stdout contract, which keeps the two artifacts decoupled and the CLI
independently contributable to PrintingPress.

## 7. Goodreads Source (two read strategies, one Source)

- **Mode selection (auto):** if a session cookie is configured → **cookie mode**
  (authenticated route, reads private shelves); otherwise → **RSS mode** (default, public
  profile, no auth, never expires).
- **RSS mode:** `https://www.goodreads.com/review/list_rss/<USER_ID>?shelf=<shelf>` per
  enabled shelf. Fields used: `title`, `author_name`, `isbn`, `book_id` (→ ExternalID).
  Caveat: the feed caps at ~100 entries per shelf → the CLI paginates (`page`/`per_page`)
  and the bridge treats pagination as the CLI's concern.
- **Cookie mode:** uses the reverse-engineered authenticated route captured via HAR;
  session supplied as `GOODREADS_SESSION` (env or GUI). On an auth failure the source
  returns a typed `ErrAuthExpired`; the engine sets `auth_status=expired` and the GUI shows
  a banner ("paste a fresh cookie") — the run is skipped gracefully, never crashes.

Both strategies are implemented inside `goodreads-pp-cli`, selected by presence of the
cookie. The bridge only sees `goodreads-pp-cli to-read --shelf X --json`.

## 8. Build-Time: Generating `goodreads-pp-cli`

One-off, AI-assisted (the only non-deterministic step):

1. Install the factory: `curl -fsSL .../cli-printing-press/main/scripts/install.sh | bash`.
2. Log into Goodreads in a browser; export a DevTools **HAR** of loading the shelves.
3. In Claude Code: `/printing-press --har ./goodreads-capture.har`.
4. The press reverse-engineers the routes and emits a Go Cobra CLI `goodreads-pp-cli`
   (agent-native flags: `--json`, `--compact`, `--dry-run`) plus an MCP server (optional).
   Credentials are read from env (`GOODREADS_SESSION`).
5. Verify the CLI returns the expected shelves (RSS + authenticated). Vendor the binary
   into the bridge's Docker build.
6. **Contribute** the result back to `mvanhorn/printing-press-library` (the current
   Goodreads entry is only a "starting map").

> ⚠️ ToS note: automating private shelves may conflict with Goodreads' Terms. This is
> Rafa's own account/data for personal use. Documented and accepted.

## 9. Shelfarr Integration

Client for `/api/v1`, Bearer token (`SHELFARR_TOKEN`).

- **Resolve:** `GET /api/v1/search?q=<isbn13>` (then isbn10, then `title author`).
- **Request:** `POST /api/v1/requests` with the resolved work id + `format=ebook`.
- **Idempotency:** treat "already requested/exists" responses as success (mark requested).
- **Known unknown (resolve in planning):** the exact `POST /api/v1/requests` payload shape
  (work id from `/search` vs raw ISBN; the `format` field name/values) — confirm against a
  live Shelfarr instance and its source. Captured as an open question (§16).

## 10. Resolver — Deterministic Matching Cascade

1. `ISBN13` exact match against `/search`.
2. `ISBN10` exact match.
3. Normalized `title + author`: lowercase, strip accents/punctuation, collapse whitespace;
   accept only if normalized title matches **and** author surname matches within a strict
   Levenshtein threshold (configurable, conservative default).
4. Otherwise → **`not_found`**, parked for manual review in the GUI.

No LLM, no opaque score. Every decision records a `MatchReason` for display. Deliberate
bias toward "not found" over a wrong match (wrong download is the worse failure).

## 11. Automation Features (deterministic; best ideas borrowed)

| Feature | Borrowed from | Behaviour |
|---|---|---|
| Scheduled list poll | Sonarr/Readarr RSS sync | Internal cron (`SCHEDULE`, default e.g. `*/30 * * * *`). |
| Manual "Sync now" | *arr | GUI button → `engine.Run`. |
| Dry-run | Bookotter | Preview what *would* be requested; nothing sent. |
| Re-check missing | *arr missing search | Re-attempt `not_found` on a slow cadence (`RECHECK_INTERVAL`). |
| Retry + backoff | *arr | Per-item isolation; a failure never aborts the whole run; max attempts then park. |
| Per-run quota | Jellyseerr quotas | `MAX_REQUESTS_PER_RUN` to avoid flooding the download client. |
| Ignore/exclusion list | *arr exclusions | Mark a book "never request" (already owned). |
| Activity history | *arr history | Deterministic log of what was requested and when. |
| First-run baseline | Bookotter-style | Default: mark existing as seen; `FIRST_RUN=backfill` to request the backlog. |
| Per-shelf format map | — | e.g. `to-read→ebook`, `audio-wishlist→audiobook`. |

## 12. Web GUI (embedded, minimal)

Served by the same binary (`go:embed` assets). Pages:

- **Dashboard** — last run, counts (requested / not-found / ignored), `auth_status` banner.
- **Shelves** — enable/disable each shelf, set per-shelf format.
- **Queue / Requested** — what's been sent, with status and history.
- **Not-found review** — resolve manually (paste correct ISBN, pick from a `/search`
  result, or ignore).
- **Settings** — Shelfarr URL/token, source mode + Goodreads cookie field, schedule,
  quotas, first-run mode.
- **Actions** — "Sync now" and "Dry run" buttons.

This is also where status (e.g. expired cookie) surfaces — in lieu of push notifications.

## 13. Configuration

Env vars seed defaults; GUI changes persist in SQLite (`/config`) and win.

```
SHELFARR_URL, SHELFARR_TOKEN
SOURCE=goodreads|hardcover            (goodreads default; hardcover fallback)
GOODREADS_USER_ID
GOODREADS_SESSION                     (optional → switches to private/cookie mode)
HARDCOVER_TOKEN                       (when SOURCE=hardcover)
SHELVES=to-read,...                   (which shelves to sync)
FORMAT=ebook                          (default; per-shelf overrides in GUI)
SCHEDULE=*/30 * * * *
MAX_REQUESTS_PER_RUN=25
RECHECK_INTERVAL=24h
FIRST_RUN=baseline|backfill           (baseline default)
```

## 14. Deployment (single Docker, Unraid-friendly)

- Multi-stage Go build → one static image (CGO-free SQLite via `modernc.org/sqlite`).
  Image contains: bridge daemon + embedded GUI + vendored `goodreads-pp-cli`.
- Volume `/config` (SQLite db + Goodreads session material).
- Internal scheduler — **no** Unraid User Scripts or Unraid-exclusive mechanisms.
- Ship an **Unraid Community Apps template (XML)** mapping the port, `/config`, and the
  env vars above — purely for convenience; the image runs anywhere Docker runs.

## 15. Error Handling

- Goodreads auth expired → typed error → `auth_status=expired`, GUI banner, run skipped
  (no crash).
- Shelfarr unreachable → retry with backoff; run marked failed in history; next tick
  retries.
- Request "already exists" (e.g. 409) → treated as success (idempotent).
- Per-item failures isolated — one bad book never aborts the run.

## 16. Open Questions (resolve during planning, not blocking the design)

1. Exact `POST /api/v1/requests` payload (work id vs ISBN; `format` field/values) — confirm
   against a live Shelfarr instance + source.
2. Exact authenticated Goodreads route for private shelves — confirm from the HAR capture.
3. Goodreads RSS pagination beyond 100 entries — verify `page`/`per_page` behaviour per
   shelf.
4. Bridge language is Go (matches the generated CLI, single static binary). If Rafa
   prefers Python to match his whisper/llama stack, revisit before the plan — but Go is the
   recommendation for a single-binary Docker.

## 17. Testing Strategy

- **Unit:** resolver cascade (fixtures: Goodreads books × Shelfarr `/search` responses);
  `store.Diff`/dedupe; config merge; per-shelf format mapping.
- **Integration:** engine against a mock Shelfarr HTTP server + a stub `goodreads-pp-cli`
  (a script emitting fixture JSON). Fully deterministic, no network.
- The generated CLI's route verification is handled by the factory (separate artifact).

## 18. Future / Out of Scope

- Contribute `goodreads-pp-cli` upstream to `printing-press-library`.
- Optional Hardcover-as-primary mode if Rafa ever migrates.
- Audiobook requests as a first-class per-shelf workflow (mechanism already present).
