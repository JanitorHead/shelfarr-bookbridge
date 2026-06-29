# Shelfarr BookBridge — Design (v2)

- **Date:** 2026-06-29
- **Status:** Approved design; hardened against verified evidence (pending final user review)
- **Owner:** Rafa
- **Supersedes:** v1 (same file). v2 folds in the code-verified Shelfarr API, native-RSS
  decision, private-via-`key` insight, the bilingual language policy, the >100 multi-strategy
  reader, status reconciliation, GUI auth, and phasing.

## 1. Problem & Goal

Rafa tracks books in **Goodreads** (primary, his living list) with a fallback export in
**Hardcover**. His downloader is **Shelfarr** ("Jellyseerr for books": searches
Prowlarr/Jackett/Newznab + direct Anna's Archive / Z-Library / LibriVox, downloads via a
torrent/usenet client, delivers to Audiobookshelf).

Nothing connects his reading lists to Shelfarr. **Bookotter** reads Hardcover only, has no
Goodreads support, and ships its own download pipeline — it cannot feed Shelfarr.

**Goal:** a self-hosted service that watches selectable Goodreads shelves and automatically
creates **ebook** download requests in Shelfarr for new books, hands-off.

## 2. Non-Goals (YAGNI)

- No indexer/download logic of our own — Shelfarr owns it.
- No push notifications (status lives in the GUI).
- No e-reader/Audiobookshelf delivery (Shelfarr's job).
- No two-way sync (never write back to Goodreads/Hardcover).
- No quiet-hours, no complex quality profiles, no LLM at runtime.

## 3. Core Principle: Deterministic Runtime

- **Build/setup (one-off, may involve AI/human):** optionally generating a private-shelf
  reader via the `cli-printing-press` factory, or capturing a session cookie. This is the
  *only* non-reproducible step, and it is **optional** (see §7).
- **Runtime (deterministic, rule-based, no LLM):** a Go daemon. Note: "deterministic" means
  the *logic* is rule-based and LLM-free and reproducible for given inputs — it does **not**
  mean results never change, because matching queries live external data (Shelfarr →
  OpenLibrary/Google Books/indexers) that evolves over time.

## 4. Verified Facts (drive the design)

### 4.1 Shelfarr REST API — verified by reading the source (Rails app)

- Base prefix **`/api/v1`** (confirmed in `config/routes.rb`; the flat `/api/...` in
  `shelfarr-plan.md` is stale/unimplemented).
- **`GET /api/v1/search?q=<str>&limit=<1..20>`** (scope `search:read`) → `{ results: [...] }`.
  Each result includes: `work_id` (`"<source>:<id>"`, source ∈ hardcover|google_books|
  openlibrary), `confidence`, `title`, `author`, `year`, `cover_url`, `has_ebook`,
  `has_audiobook`, `editions[]`, `sources[]`, `series_name`, `series_position`.
- **`POST /api/v1/requests`** (scope `requests:write`). Required: `work_id` **and**
  `book_type` (or `book_types[]`). Optional: `title`, `author`, `cover_url`, `year`,
  `language` (ISO 639-1), `notes`, `source_work_ids[]`. `book_type` enum = `ebook` |
  `audiobook`. The `work_id` comes from a search result — **not** a raw ISBN.
- **`GET /api/v1/requests/:id`** → `status` ∈ `pending|searching|not_found|downloading|
  processing|completed|failed`, plus `attention_needed`, `issue_description`, timestamps,
  nested `book`/`request`/`user`. `GET /api/v1/requests?status=&limit=` lists.
  `DELETE /api/v1/requests/:id` cancels; `POST /api/v1/requests/:id/retry` (admin).
- **Auth:** `Authorization: Bearer shf_…`. Per-user scoped tokens created at
  **Profile → API tokens**; self-service scopes = `search:read`, `requests:read`,
  `requests:write` (exactly what we need).
- **Language is a *soft* preference, not a hard veto** (verified in `release_scorer.rb` /
  `search_result.rb`):
  - `scope :matches_language, ->(lang) { where(detected_language: [lang, nil]) }` — releases
    with **no detected language always pass**; only releases explicitly tagged a *different*
    language are excluded from auto-select.
  - Scoring weight: `language: 25` of 100 (match=100, unknown/multi=50, wrong=0).
  - Omitting `language` → Shelfarr fills `effective_language` from its global
    `default_language` setting.

### 4.2 Goodreads reading — verified via dev-forum + field reports

- **Per-shelf RSS:** `https://www.goodreads.com/review/list_rss/<USER_ID>?shelf=<slug>`.
  Fields we rely on: `book_id` (**always present → our dedup/join key**), `title`,
  `author_name`, `isbn` (**often empty**; **no `isbn13`, no `asin`**), `user_date_added`,
  `user_shelves`. Titles/descriptions are HTML-encoded / CDATA → must decode + strip tags.
- **Public vs private:** plain RSS needs a public profile; a **private** profile is readable
  by appending the per-user **`key=` feed token** from the logged-in RSS link (durable, no
  expiry) — for shelves **≤100 items**. The `key` is a secret → redact in logs/config.
- **Hard 100-item cap on RSS**, confirmed on Goodreads' dev forum; `page=`/`per_page=` do
  **not** page past it. >100 requires the **HTML list** route
  `https://www.goodreads.com/review/list/<USER_ID>?shelf=<slug>&page=N&per_page=…`
  (public profile → no auth; private profile → needs a session cookie).
- **Politeness:** Cloudflare-fronted; no documented limit and no reliable conditional GET
  (ETag/If-Modified-Since). Poll on the order of hours, send a real User-Agent, back off on
  errors, dedupe client-side by `book_id` + `user_date_added`.
- **Shelf slugs** are lowercase-hyphenated and are **account inventory → discover, don't
  hardcode** (`#ALL#` is the pseudo-shelf for everything).

### 4.3 `cli-printing-press` maturity

Real, active factory (Go CLI + MCP generator). Its Goodreads package has generated code but
the authors' own notes call it a *"starting map"*: local checks pass, **live parsing is
unverified**, writes untested, and HTML drift breaks parsers silently. → We do **not** hard-
depend on it. Native RSS is the core; the factory/cookie path is an **optional, isolated**
strategy for private >100 (§7), and a candidate to contribute back upstream.

### Rejected alternatives
- **RapidAPI Goodreads wrappers** — structurally cannot read a *private* shelf (no user
  session) and redundant for a *public* one (free RSS already works). Strictly dominated.
- **Bookotter fork** — no native Goodreads, Kindle-SSH baggage, foreign code to gut.

## 5. Architecture Overview

One Go binary → one Docker image: daemon + embedded authenticated GUI + SQLite state +
internal cron scheduler. Optional vendored private-reader artifact (Phase 2).

```
            ┌────────────────────────── 1 Docker container ──────────────────────────┐
 Goodreads  │  sources/goodreads ──Book[]──┐                                          │
 (RSS pub / │   (strategy: RSS | RSS+key |  │   store.Diff (SQLite, dedupe by book_id) │
  RSS+key / │    HTML pub | HTML cookie)    │            │                             │
  HTML)     │                               ▼            ▼                             │
            │  sources/hardcover [Phase 2]  resolver ── shelfarr client (/api/v1) ─────│──▶ Shelfarr
            │   (GraphQL)        langdetect(title)│   search→work_id→request(ebook,lang)│     /api/v1
            │                                     ▼                                     │
            │             scheduler(cron) ─▶ engine.Run() ─▶ reconcile(status) ◀────────│──  Shelfarr
            │             web GUI (go:embed, auth) ◀── reads/controls store             │     status
            │             volume /config (SQLite + secrets)                             │
            └─────────────────────────────────────────────────────────────────────────┘
```

### Engine loop

```
tick | "Sync now" → engine.Run(dryRun):
  books = source.Fetch(enabledShelves)              // best strategy per (visibility,size)
  new   = store.Diff(books)                          // dedupe by (source, book_id); skip ignored; baseline
  for b in new[:MAX_REQUESTS_PER_RUN]:
      lang  = langdetect(b.title) if confident else nil      // soft, optional
      res   = shelfarr.Search(b.isbn || "b.title b.author")  // ISBN query when present
      pick  = bestEbook(res)                                  // confidence ≥ threshold AND has_ebook
      if pick and not dryRun:
          id = shelfarr.CreateRequest(pick.work_id, "ebook", lang, b.title, b.author)
          store.MarkRequested(b, id)
      elif pick and dryRun: store.Preview(b, pick)
      else: store.MarkNotFound(b)                             // → GUI review queue
  reconcile: for r in store.Open(): s = shelfarr.GetRequest(r.id)   // update / resurface failed|not_found
  reattempt store.NotFound(olderThan RECHECK_INTERVAL)        // slow cadence
  store.RecordHistory(summary)
```

## 6. Components (each independently testable)

| Module | Responsibility | Key interface |
|---|---|---|
| `sources/` | Fetch normalized `Book`s from a provider, best strategy per situation | `Source.Fetch(ctx, shelves) ([]Book, error)` |
| `sources/goodreads` | RSS / RSS+key / HTML-public / HTML-cookie strategies; slug discovery; HTML-entity decode | strategy auto-select by config+size |
| `sources/hardcover` *(Phase 2)* | GraphQL token source | same `Source` iface |
| `langdetect/` | Deterministic language of a title, confidence-gated | `Detect(title) (lang string, ok bool)` |
| `store/` | SQLite: items, history, ignore list, settings | CRUD + `Diff([]Book) []Book` |
| `shelfarr/` | Typed `/api/v1` client | `Search(q,limit)`, `CreateRequest(...)`, `GetRequest(id)` |
| `resolver/` | Pick best search result → request; bias to not-found | `Resolve(Book, ShelfarrClient) (*Pick, Reason)` |
| `engine/` | Orchestrate fetch→diff→resolve→request→reconcile; quota, retry, dry-run | `Run(ctx, opts) RunReport` |
| `scheduler/` | Cron-driven engine invocation | `Start(cronExpr, engine)` |
| `web/` | Embedded GUI + HTTP handlers, **with auth** | net/http + `go:embed` |
| `config/` | Merge env + persisted settings (GUI wins) | `Load() Config` |
| `cmd/bookbridge` | Wire + start scheduler + web | — |

`Book` (normalized): `{Source, ExternalID(=book_id), Title, Author, ISBN10, Shelf, AddedAt}`.

## 7. Goodreads Source — "option for everything"

Supports all four read situations, auto-selecting the most robust mechanism:

| | ≤100 items / shelf | >100 items / shelf |
|---|---|---|
| **Public profile** | RSS (stable) | HTML list pagination, **no cookie** |
| **Private profile** | RSS + `key=` (durable) | authenticated HTML + session cookie *(Phase 2; isolated; the only fragile path)* |

- Strategy chosen from config (`public` / `key` / `cookie`) and observed shelf size (RSS
  until it returns ~100, then HTML). The fragile cookie path is encapsulated behind the
  `Source` interface so a break degrades gracefully.
- **Join key = `book_id`** (ISBN may be empty). HTML-entity/CDATA decode on titles. Shelf
  slugs discovered from the account, not hardcoded. Secrets (`key`, cookie) redacted in logs.

## 8. Resolver & Language Policy

1. Query Shelfarr `search`: `q = ISBN10` when present, else `"<title> <author>"`.
2. Pick the result with `has_ebook == true` and highest `confidence`; accept only if
   `confidence ≥ MATCH_THRESHOLD` (conservative default). Else → **`not_found`** (manual
   review in GUI). Deliberate bias: a wrong download is worse than a manual review.
3. **Language (bilingual-aware, soft):** run `langdetect(title)`. If confident → send
   `language=<es|en|…>` (favors that language; untagged ebooks still pass; only the
   explicitly-other-tagged are excluded). If not confident ("a veces no") → omit `language`
   → fully permissive. Inference is **configurable/toggleable**; default on.
4. `POST /api/v1/requests { work_id, book_type:"ebook"(per-shelf override), language?, title,
   author }`. Treat "already exists" as success (idempotent).

## 9. Status Reconciliation

Each run polls `GET /api/v1/requests/:id` for open items and maps Shelfarr `status` into our
store: `completed` → done; `not_found`/`failed` → resurface (eligible for slow recheck and
shown in GUI); `attention_needed` → flag in GUI. Closes the fire-and-forget gap and powers a
GUI that shows *real* download state, not just "sent".

## 10. Automation (deterministic; best ideas borrowed)

| Feature | From | Behaviour |
|---|---|---|
| Scheduled poll | Sonarr/Readarr | Internal cron; **hourly default** (polite to Goodreads). |
| Manual Sync / Dry-run | *arr / Bookotter | GUI buttons. |
| Re-check missing | *arr | Re-attempt `not_found`/`failed` on `RECHECK_INTERVAL`. |
| Retry + backoff | *arr | Per-item isolation; one failure never aborts a run. |
| Per-run quota | Jellyseerr | `MAX_REQUESTS_PER_RUN`. |
| Ignore list | *arr exclusions | "never request" (already owned). |
| Activity history | *arr | Deterministic log. |
| First-run baseline | Bookotter | Default: mark existing seen; `FIRST_RUN=backfill` to request the backlog. |
| Per-shelf format/lang | — | e.g. `to-read→ebook`, an audio shelf→audiobook; optional per-shelf language override. |

## 11. Web GUI (embedded, authenticated)

Served by the same binary (`go:embed`). **Access-controlled** (set password / basic auth;
or bind localhost + documented reverse proxy) because it holds the Shelfarr token + Goodreads
key/cookie and can trigger requests. Pages: **Dashboard** (last run, counts, status/auth
banner) · **Shelves** (enable + format + optional language) · **Queue/Requested** (real
Shelfarr status) · **Not-found review** (resolve via ISBN/search pick, or ignore) ·
**Settings** (Shelfarr URL/token, source mode + key/cookie, schedule, quota, first-run,
language-inference toggle) · **Actions** (Sync now / Dry-run).

## 12. Configuration

Env seeds defaults; GUI changes persist in SQLite (`/config`) and win. Startup guard warns
if `/config` looks empty/unmounted (prevents accidental re-baseline / mass re-request).

```
SHELFARR_URL, SHELFARR_TOKEN            (Bearer shf_…)
GOODREADS_USER_ID
GOODREADS_VISIBILITY=public|private
GOODREADS_FEED_KEY                      (private ≤100)
GOODREADS_COOKIE                        (private >100, Phase 2; optional)
SHELVES=to-read,...                     (discovered list; user selects)
FORMAT=ebook                            (per-shelf overrides in GUI)
LANG_INFERENCE=on|off                   (default on)
SCHEDULE=0 * * * *                      (hourly default)
MAX_REQUESTS_PER_RUN=25
MATCH_THRESHOLD=<conservative>
RECHECK_INTERVAL=24h
FIRST_RUN=baseline|backfill             (baseline default)
GUI_PASSWORD=...                        (required to expose beyond localhost)
```

## 13. Deployment (single Docker, Unraid-friendly)

Multi-stage Go build → one static image (CGO-free SQLite via `modernc.org/sqlite`); contains
daemon + embedded GUI (+ optional vendored private-reader in Phase 2). Volume `/config`.
**Internal scheduler — nothing Unraid-exclusive.** Ship an Unraid Community Apps template
(XML) mapping port, `/config`, and the env above. Runs anywhere Docker runs.

## 14. Error Handling

- Goodreads auth/key invalid → typed error → GUI banner; run skipped, no crash.
- Shelfarr unreachable → retry w/ backoff; run marked failed in history; next tick retries.
- "Already exists" → treated as success (idempotent).
- Per-item failures isolated.
- Secrets never logged (Bearer token, RSS `key`, cookie redacted).

## 15. Security

- GUI authentication required to expose beyond localhost; secrets stored in `/config`,
  redacted in logs.
- The optional private-reader (factory-generated or cookie HTML) is AI-/reverse-engineered
  code touching an authenticated Goodreads session → review before trusting; native RSS
  avoids this for the default paths.
- ToS note: automating authenticated/private Goodreads access may conflict with Goodreads'
  Terms. Personal use of own account/data. Documented and accepted.

## 16. Testing Strategy

- **Unit:** resolver pick/threshold; `langdetect` confidence gating (es/en/ambiguous
  fixtures); `store.Diff`/dedupe by `book_id`; RSS parser against **messy real-world
  fixtures** (empty ISBN, HTML entities/CDATA, custom slugs, ~100-cap); per-shelf
  format/language mapping.
- **Integration:** engine against a **mock Shelfarr HTTP server** + a **stub Goodreads
  reader** emitting fixtures. Fully deterministic, no network. Includes reconciliation paths.
- **E2E milestone (Phase 0):** one real request against a live Shelfarr instance to confirm
  search→work_id→request→status end-to-end before building the rest.

## 17. Phasing

- **Phase 0 — De-risk:** confirm `search → work_id → request → status` against a live
  Shelfarr; verify a Goodreads HTML-list page parses for the >100 shelf.
- **Phase 1 — MVP:** native RSS (public any-size via HTML pagination; private ≤100 via
  `key`) → dedupe → resolver + language inference → ebook request → reconciliation → authed
  GUI → single Docker + Unraid template. Baseline first-run, quota, retry, ignore, recheck.
- **Phase 2:** private >100 via authenticated cookie HTML (isolated); Hardcover source;
  advanced automation (per-shelf audiobook); contribute the Goodreads reader to
  `printing-press-library`.

## 18. Open Questions (resolve in planning)

1. Exact semantics/scale of search `confidence` (is it the metadata-match score, and what
   threshold reliably avoids wrong picks?) — calibrate `MATCH_THRESHOLD` against real data.
2. Goodreads HTML-list `per_page` max and exact pagination behavior for a public profile.
3. `langdetect` library choice and accuracy on short titles (e.g. `lingua-go`) + the
   confidence cutoff.
4. Whether requesting `ebook` reliably yields a usable format (epub) from Shelfarr's direct
   sources for Spanish titles.
