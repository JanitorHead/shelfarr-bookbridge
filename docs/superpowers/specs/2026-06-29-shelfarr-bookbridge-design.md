# Shelfarr BookBridge — Design (v3)

- **Date:** 2026-06-29
- **Status:** Approved, hardened against a multi-agent audit (88 findings) + 3 owner decisions.
- **Owner:** Rafa
- **Supersedes:** v2. v3 fixes four verified-false premises, adopts a CLI-first MVP, moves
  the authenticated-cookie reader into Phase 1 (the owner is private + >100), keeps
  langdetect, and pins an explicit data model, crash-safety, concurrency and security model.

## 1. Problem & Goal

Connect Rafa's **Goodreads** shelves to his **Shelfarr** downloader: watch selectable
shelves and automatically create **ebook** download requests in Shelfarr for new books,
hands-off, self-hosted on Unraid in one Docker container. Bookotter can't do this (Hardcover-
only, own pipeline). Shelfarr exposes a verified REST API for exactly this automation.

## 2. Owner Decisions (v3)

1. **Read path for >100 private shelves = authenticated session cookie** (the public HTML
   route is now login-gated; see §4.2). The fragile path is unavoidable for his situation;
   we mitigate with robust stale-cookie detection + clear re-auth UX.
2. **CLI-first MVP.** Phase 1 is a config-file + `--dry-run`/`--apply` CLI run by cron in one
   Docker container. The embedded web GUI (and its auth/CSRF/session surface) defers to
   Phase 2.
3. **Keep language inference** (lingua-go) in the MVP, confidence-gated (§8).

## 3. Core Principle: Deterministic Runtime

Runtime logic is rule-based and LLM-free (reproducible for given inputs); it is **not**
result-stable, because matching queries live external data (Shelfarr → metadata providers /
indexers) that changes over time. The only AI/human build-time step is the optional Phase-2
factory reader, which is feature-flagged off and isolated.

## 4. Verified Facts (code-/empirically-confirmed; drive the design)

### 4.1 Shelfarr REST API (Rails; read from source)

- Prefix **`/api/v1`** (the flat `/api/...` in `shelfarr-plan.md` is unimplemented).
- **`GET /api/v1/search?q=&limit=1..20`** (scope `search:read`) → `{ results: [...] }`. Each
  result: `work_id` (`"<source>:<id>"`, source ∈ hardcover|google_books|openlibrary; **may
  be a legacy un-prefixed OpenLibrary id with no colon**), `confidence`, `title`, `author`,
  `year`, `cover_url`, `has_ebook`, `has_audiobook`, `editions[]`, `sources[]`,
  `series_name`, `series_position`.
- **`confidence` is a cross-provider CORROBORATION score, not query relevance** — discrete
  `{70,90,100}` (100 = 2+ providers + ISBN; 90 = 2+ providers; 70 = single provider),
  **config-dependent** (all results = 70 if only one metadata provider is enabled) and **may
  be null**. The query text never enters its computation. → Use it ONLY as a secondary
  tiebreaker (prefer multi-source-corroborated). Evidence: `aggregator.rb:132-137`.
- **`has_ebook`/`has_audiobook` are tri-state HINTS** (true/false/**nil**), not download
  guarantees: OpenLibrary returns nil for both; Google Books nil for audiobook. Treat nil as
  UNKNOWN; do **not** coerce to false (would drop every OpenLibrary ebook). Evidence:
  `aggregator.rb:101-106`, `result_normalizer.rb:60-61,81-83`.
- **`POST /api/v1/requests`** (scope `requests:write`). Required: `work_id` **and**
  `book_type`|`book_types[]` (enum `ebook`|`audiobook`). Optional: `title`, `author`,
  `cover_url`, `year`, `language` (ISO 639-1), `notes`, `source_work_ids[]`.
  - **Not idempotent.** A duplicate (active request or already-acquired) → **HTTP 422** with
    the reason in `errors[]` and **does not return the existing request id**; no server-side
    unique index (TOCTOU). A multi-`book_type` request can **partially succeed** (201 with a
    populated `errors[]`). Response shape `{ requests:[...], warnings:[...], errors:[...] }`
    — inspect both arrays. Evidence: `request_creation_service.rb:43-46`,
    `requests_controller.rb:45-51`, `duplicate_detection_service.rb:56-67`.
- **`GET /api/v1/requests/:id`** (scope `requests:read`) → `status` ∈ `pending|searching|
  not_found|downloading|processing|completed|failed`, `attention_needed`, `issue_description`,
  timestamps. A cancelled/`DELETE`d request → **404**; `cancel!` sets status **`failed`**
  (there is no distinct `cancelled` status). `GET /api/v1/requests?status=&limit=` (cap 100)
  lists — use for batch reconciliation. `POST /api/v1/requests/:id/retry` needs
  **`requests:admin`** (NOT self-service) → BookBridge must not depend on it.
- **Language is a soft 25/100 scoring weight, NOT a hard filter.** `release_scorer.rb`:
  match=100, **multi-language=100**, unknown/undetected=50, wrong=0. The `matches_language`
  scope exists only in `search_result.rb:71-78` and is **not** chained into `auto_selectable`
  (which applies only `high_confidence`) — a wrong-language release can still auto-select if
  it clears the threshold. **For direct-source ebooks the detected language is overwritten to
  nil** (`search_job.rb`), so any language passes → language inference buys little for ebooks
  (kept anyway per owner decision; see §8).
- **Auth:** `Authorization: Bearer shf_…`. Per-user token at **Profile → API tokens**.
  **Required scopes: `search:read`, `requests:write`, `requests:read`** (all self-service).
- **No server-side API rate limiting** → we self-throttle; respect `search` limit cap 20 and
  `requests` page cap 100.

### 4.2 Goodreads reading (empirically verified 2026-06-29)

- **RSS** `https://www.goodreads.com/review/list_rss/<USER_ID>?shelf=<slug>` is the **only
  anonymous path** and has a **hard ~100-item cap** (`page`/`per_page` do not beat it).
  Private shelves readable by appending the durable per-user **`key=`** feed token (≤100).
  Fields: `book_id` (**always present → canonical id**), `title`, `author_name`, `isbn`
  (**often empty; no isbn13/asin**), `user_shelves` (plural), `user_date_added`. Titles are
  HTML-encoded/CDATA → decode + strip tags. RSS carries no total → a return count **== 100 is
  "possibly truncated"** and must escalate.
- **The public HTML-list route `review/list/<id>` now 302-redirects to `/user/sign_in` even
  for public profiles** (verified on public ids 1 and 21945567). → **All >100 reads (public
  AND private) require a logged-in session cookie.** Pagination that works (with cookie):
  `?shelf=<slug>&per_page=100&page=N&print=true`; stop on sentinel
  `div.greyText.nocontent.stacked` (or empty `tbody#booksBody`), not on a large per_page.
  Stable selectors (ref: maintained scraper `YashTotale/goodreads-user-scraper`):
  rows `tbody#booksBody > tr`; `book_id` = last path segment of `td.field.title div.value
  a@href` (`/book/show/<id>`); title same anchor; author `td.field.author div.value a`; isbn
  `td.field.isbn div.value`; isbn13 `td.field.isbn13 div.value` (each cell optional/user-
  toggleable — skip malformed rows). Cookie is short-lived (rotates on logout/password-
  change); detect the sign-in wall (`<title>Sign in</title>` / `div#third_party_sign_in`) and
  raise a clear "cookie expired" error rather than treating it as an empty shelf.
- **Politeness:** Cloudflare-fronted, no conditional GET. Poll hourly by default; 1–3s
  jittered inter-page delay; honor 429/`Retry-After`; real User-Agent; host allowlist
  (`goodreads.com`/`www.goodreads.com`), no cross-host redirects.
- Shelf slugs are lowercase-hyphenated **account inventory → discover, don't hardcode**
  (`#ALL#` = everything). The public `user/show/<id>` page exposes shelf names + counts in
  `div#shelves` (loads without cookie for public profiles) → use to predict the 100-cap.

### 4.3 Ebook acquisition reality (Open Question #4, resolved)

Requesting `ebook` yields **epub or pdf only**, from the **direct sources** (Anna's Archive,
Z-Library) — mobi/azw3 never requested (those need the indexer/torrent path). It is
**best-effort and can silently yield zero**: only ONE direct source runs per request
(Z-Library is a config fallback when Anna's is unconfigured, **not** on Anna's runtime
failure), and Anna's needs **FlareSolverr** or raises BotProtectionError. Spanish is handled
(`es` for Anna, `spanish` for Z-Library). → Send the **Spanish-edition title** as request
metadata for recall; treat direct-ebook outcomes as possibly needing recheck/manual handling
(reconciliation surfaces `not_found`/`failed`).

### Rejected: RapidAPI (can't read private; redundant for public). Bookotter fork (no Goodreads).

## 5. Architecture (CLI-first, one Docker)

One Go binary, two entry modes sharing all internals:
- **daemon** — internal cron runs `engine.Run` on `SCHEDULE`.
- **CLI** — `bookbridge sync --dry-run|--apply`, `bookbridge shelves`, `bookbridge reconcile`.

```
 Goodreads ──▶ sources/goodreads ─Book[]─▶ store.Diff(SQLite, key=(source,book_id))
   RSS(±key) | cookie-HTML            │           │
                                      ▼           ▼
            langdetect(title) ─▶ resolver(pure) ─▶ shelfarr client ─▶ /api/v1 search→request
                                      │                                        │
            scheduler(cron)/CLI ─▶ engine.Run ─▶ reconcile(status, batched) ◀──┘ status
            single-flight lock · /config volume (SQLite WAL + secrets 0600)
```

### Engine — decomposed into testable phases, local store is the source of truth

```
engine.Run(opts):  [single-flight lock; one Run at a time]
  fetchPhase:    books = source.Fetch(enabledShelves)         // best strategy per (visibility, observed size)
  diffPhase:     new   = store.Diff(books)                     // by (source, book_id); skip ignored; per-shelf baseline
  requestPhase:  for b in new[:quota]:
                   res  = shelfarr.Search(b.isbn || "b.title b.author")
                   pick = resolver.Resolve(b, res, cfg)        // PURE: self-similarity; nil ⇒ not_found
                   if pick && apply:
                      store.Mark(b, state=requesting, work_id=pick.work_id)   // intent BEFORE POST
                      r = shelfarr.CreateRequest(params)                       // 422-"exists" ⇒ lookup existing
                      store.Mark(b, state=requested, request_id=r.id)
                   else if pick && dryRun: store.Preview(b, pick)
                   else: store.Mark(b, state=not_found)
  reconcilePhase: batch GET /requests?status= → update items; 404 ⇒ cancelled-externally (don't re-request our cancels)
  recheckPhase:  bounded re-search of not_found/failed by attempt_count + aging; terminal 'parked' state
  startup:       reconcile dangling state=requesting intents via GET /requests?status= (never blind re-request)
```

## 6. Components (independently testable)

| Module | Responsibility | Interface |
|---|---|---|
| `sources/goodreads/fetch` | Page retrieval + **auth as a decorator**: none / `key` query-param / cookie header; host allowlist; backoff | `Fetch(shelf, page)` |
| `sources/goodreads/parse` | **two parsers** (rss, html) → ONE shared canonical `ExternalID` extractor | `Parse(bytes) []Book` |
| `sources/goodreads` | selector picks (parser, auth) from (visibility, observed size); RSS until ~100 then cookie-HTML | `Source.Fetch(ctx, shelves) ([]Book, error)` |
| `langdetect` | deterministic title language, confidence-gated | `Detect(title) (lang string, ok bool)` |
| `store` | SQLite state (§6.1), `Diff`, single-flight lock, migrations | CRUD + `Diff([]Book) []Book` |
| `shelfarr` | typed `/api/v1` client; `CreateRequestParams` struct | `Search`, `CreateRequest`, `GetRequest`, `ListRequests` |
| `resolver` | **PURE** `Resolve(Book, []SearchResult, cfg) (*Pick, Reason)` | no I/O |
| `engine` | orchestrate phases; quota, retry, dry-run; RunReport; `Clock` injected | `Run(ctx, opts) RunReport` |
| `scheduler` | cron (UTC + `CRON_TZ`); `Reschedule()` on SCHEDULE change | — |
| `config` | merge env + persisted settings; precedence rules (§12) | `Load() Config` |
| `cmd/bookbridge` | daemon + CLI subcommands | — |

`Book` = `{Source, ExternalID(=book_id), Title, Author, ISBN10, Shelves []string, AddedAt,
Year, CoverURL}`. **Identity = (source, external_id)**; `AddedAt`/`user_date_added` is
ordering/baseline only, never identity (removed-then-re-added is NOT re-requested; ignore
list survives).

### 6.1 SQLite schema (explicit)

- `books(PK (source, external_id), title, author, isbn10, year, cover_url, added_at,
  first_seen_at, state ENUM[new|baseline|requesting|requested|not_found|failed|done|parked|
  ignored], work_id, chosen_language NULL, chosen_format, shelfarr_request_id NULL,
  last_status, attention_needed, issue_description, attempt_count, last_checked_at,
  created_at, updated_at)`
- `book_shelves(source, external_id, shelf)` — many-to-many (a book on several shelves).
- `shelf_config(shelf PK, enabled, baselined_at NULL, format, language NULL)`.
- `ignores(source, external_id UNIQUE)`.
- `history(id, started_at, finished_at, trigger, fetched, new, requested, not_found, failed,
  summary_json)`.
- `settings(key PK, value)`; `run_state(singleton: running, started_at)`;
  schema version via `PRAGMA user_version`.
- Indexes: `UNIQUE(source,external_id)`, `books(state)`, `books(shelfarr_request_id)`,
  `books(state,last_checked_at)`, `history(started_at)`. **WAL + `busy_timeout`; single
  serialized writer** (modernc.org/sqlite is single-writer).

## 7. Goodreads Source — strategy & invariants

| | ≤100 items / shelf | >100 items / shelf |
|---|---|---|
| **Public** | RSS (no auth) | **cookie-HTML** (public route is login-gated now) |
| **Private** | RSS + `key=` (durable) | **cookie-HTML** (owner's case) |

- The "four strategies" are really **two parsers × three auth modes**; model them that way.
- **Invariant (contract test):** the RSS parser and the HTML parser MUST emit **identical
  `ExternalID` sets** for the same shelf — otherwise crossing the 100-cap makes every book
  re-appear as "new" and mass-duplicates requests. This is the single most dangerous coupling.
- Exactly-100 RSS return → "possibly truncated" → escalate to cookie-HTML; if no cookie
  configured, surface a loud warning, never silently drop. Predict via `user/show/<id>` counts.
- Cookie supplied as a secret (`GOODREADS_COOKIE`, literal `Cookie` header, no jar mutation);
  sign-in-wall detector → fall back to 100-item RSS + "cookie expired, re-grab from DevTools".

## 8. Resolver & Language (corrected)

1. Query Shelfarr `search`: `q = ISBN10` when present, else `"<title> <author>"`.
2. **`resolver.Resolve` is a pure function.** Compute our **own normalized title+author
   similarity** (substring + trigram, mirroring Shelfarr's `ReleaseScorer`) between the
   Goodreads book and each result. Pick the best; accept only if similarity ≥
   **`SIMILARITY_THRESHOLD`** (self-computed, conservative). Else → `not_found` (bias: a wrong
   download is worse than a skip). **Do not** threshold on Shelfarr `confidence` (use only as
   a tiebreaker, never gate above 70). **Do not** hard-gate on `has_ebook` (nil = unknown —
   request and let Shelfarr confirm availability via reconciliation).
3. **Language (kept; near-moot for ebooks but cheap):** lingua-go,
   `.FromLanguages(English, Spanish)`, **high-accuracy** (no LowAccuracyMode), pinned version.
   Decide via `ComputeLanguageConfidenceValues`: accept top only if `top ≥ 0.65` AND
   `top − second ≥ 0.15`; else **omit** `language` (a first-class outcome). Detect on the
   **title alone**. Configurable on/off; default on.
4. Submit `CreateRequestParams{ work_id, book_types:[per-shelf format, default ebook],
   language?, title, author, cover_url, year, source_work_ids[] }`. Persist the chosen
   `work_id`; never re-resolve an item that already has an open/completed request. Handle:
   422-"already exists" → no-op + look up the existing request via `GET /requests?status=`;
   201-with-`errors[]` → partial success (record per book_type); serialize submissions per
   `(work_id, book_type)` (no server unique index). `work_id` may be colon-less (legacy OL).

## 9. Status Reconciliation

Each run **batch**-polls `GET /api/v1/requests?status=&limit≤100` (not per-item) for open
items → map Shelfarr `status` into the store; `completed` → done; `not_found`/`failed` →
eligible for bounded recheck; `attention_needed` → flag. **404** → our DELETE or an external
cancel: track local cancel intent so we don't re-request our own cancellations; distinguish
404 from transient 5xx. Bound rechecks with `attempt_count` + aging cadence + terminal
`parked` state; process new books before rechecks; rechecks share the per-run quota.

## 10. Automation (deterministic)

Cron poll (hourly default) · `--dry-run`/`--apply` · per-item retry+backoff (failure never
aborts a run) · `MAX_REQUESTS_PER_RUN` quota · ignore list · activity history · **per-shelf
baseline** first-run (`baseline` default vs `backfill`; applied when a shelf first becomes
enabled, with a count shown before enabling) · per-shelf format/language overrides with
**deterministic precedence** when a book sits on multiple enabled shelves (configured shelf-
priority order decides; state which wins) · bounded recheck of not_found/failed.

## 11. CLI & (Phase 2) GUI

**Phase 1 CLI:** `sync --dry-run|--apply`, `shelves` (discover/enable/configure), `reconcile`,
`status`. Config via env + a `/config` file, persisted in SQLite. No web surface.
**Phase 2 GUI:** embedded `go:embed`, with a real **login-form + server-side session**
(HttpOnly/Secure/SameSite=Strict cookies, argon2id password hash, first-run forced password,
login lockout), **CSRF** on all mutating POSTs, **write-only secret fields** (masked, never
echoed), security headers (CSP/X-Frame-Options/nosniff). Fail-closed: refuse non-loopback
bind without a password.

## 12. Configuration

Env **seeds** settings; once persisted in SQLite the **store wins and later env changes are
ignored** (surface effective config via `bookbridge status`). Times in **UTC**; `CRON_TZ` for
the schedule; `scheduler.Reschedule()` on SCHEDULE change.

```
SHELFARR_URL, SHELFARR_TOKEN            (Bearer shf_…; scopes search:read,requests:write,requests:read)
GOODREADS_USER_ID
GOODREADS_VISIBILITY=public|private
GOODREADS_FEED_KEY                      (private ≤100)
GOODREADS_COOKIE                        (>100, owner's case; secret)
SHELVES=<discovered; user selects>      FORMAT=ebook (per-shelf overrides)
LANG_INFERENCE=on  SIMILARITY_THRESHOLD=<conservative>
SCHEDULE=0 * * * *  CRON_TZ=...  MAX_REQUESTS_PER_RUN=25  RECHECK_INTERVAL=24h
FIRST_RUN=baseline|backfill  SHELFARR_INSECURE=false
```

## 13. Deployment

Multi-stage Go build → one static image (CGO-free `modernc.org/sqlite`); **runs as non-root**;
volume `/config` (SQLite `0600`, secret files `0700`). Internal scheduler — nothing Unraid-
exclusive. Unraid Community Apps template (XML) maps `/config` + env. (FlareSolverr is a
separate container Shelfarr already needs for Anna's Archive — documented as a prerequisite.)

## 14. Error Handling

- Goodreads cookie/key invalid → typed error → log/banner; run skipped, no crash; fall back
  to RSS where possible.
- **Parse-sanity:** a previously-non-empty shelf parsing to 0 (or a sharp drop) → treat as a
  probable parse error → skip the diff for that shelf + alert (don't act on the empty result).
- Shelf rename/delete: reconcile enabled shelves against the live slug list each discovery;
  flag missing slugs instead of fetching a dead one.
- Shelfarr unreachable → retry w/ backoff; run marked failed in history; next tick retries.
- 422-"already exists" → no-op + lookup (NOT treated as a hard error).
- Per-item failures isolated.

## 15. Security (CLI-scoped; web items deferred with the GUI)

- **Secrets at rest:** SQLite `0600`; secret columns (Shelfarr token, feed key, cookie)
  encrypted with an env master key **or** stored in a separate `0600` file distinct from the
  state DB (so Unraid parity/snapshots/backups don't leak credentials); non-root UID; warn
  loudly if the secrets file is group/world-readable.
- **Redaction:** a `SecretString` type whose `String()/MarshalJSON`/format verbs emit `***`;
  strip query strings (the RSS URL carries `?key=`) and `Authorization`/`Cookie` headers from
  any echoed URL/error/dry-run output; a test scans all log/error/output for fixture secrets.
- **Transport:** refuse `http` SHELFARR_URL on a non-loopback host unless `SHELFARR_INSECURE=
  true` (the `shf_` token grants `requests:write`); recommend https across hosts.
- **Egress/parsing hardening:** Goodreads host allowlist + no cross-host redirects; Shelfarr
  client calls only the configured base + known `/api/v1` paths, no server-controlled
  redirects; never auto-fetch `cover_url`; `encoding/xml` resolves no external entities; cap
  body size + parse depth; URL/query-encode search queries; malicious-title & entity-bomb
  fixtures. (Phase-2 GUI: render Goodreads-derived strings via `html/template` auto-escaping.)
- **Schema migration:** `PRAGMA user_version` + a startup migration runner; **fail-closed**
  (refuse + alert) on an unknown/newer schema rather than re-baselining; the `/config` startup
  guard checks schema match, not just emptiness.
- **ToS:** default to lowest-risk RSS; the cookie path is opt-in, scoped to the owner's own
  account, with a ban-risk note and enforced polite polling (hourly + jitter + hard minimum +
  real UA). The Phase-2 factory reader stays **off by default**, isolated behind `Source`,
  with **no access to the Shelfarr token**, pinned/checksummed, manual-review-gated.

## 16. Testing

- **Unit:** `resolver` (pure, table-driven: similarity threshold, tiebreaks, not_found);
  `langdetect` gating (es/en/ambiguous/proper-noun titles); `store.Diff`/dedup by id; RSS
  parser on **messy real fixtures** (empty ISBN, entities/CDATA, custom slugs, exactly-100);
  HTML parser on captured shelf pages (sentinel/pagination); per-shelf format/language
  precedence.
- **Invariants:** **RSS↔HTML ExternalID-equivalence** (same shelf, same id set);
  exactly-100 escalation.
- **Integration:** a **programmable httptest mock Shelfarr** scripting per-id status
  progression (pending→searching→downloading→completed; plus not_found/failed/attention/404),
  pinned to a **real response fixture captured in Phase 0**; engine phases driven by a **fake
  `Clock`**. Includes intent-row crash-recovery and 422-duplicate paths.

## 17. Phasing

- **Phase 0 — De-risk (before planning code):** one live Shelfarr round-trip
  (search→work_id→request→status), capture JSON fixtures; verify the **cookie-HTML parser on
  the owner's actual >100 private shelf** (the only viable read path).
- **Phase 1 — CLI MVP (one Docker):** reader (RSS ≤100 ±key; cookie-HTML >100 with sign-in-
  wall detection) · pure resolver (self-similarity) + langdetect · request submission (intent
  rows, 422/partial handling, per-(work_id,book_type) serialization) · batch reconciliation ·
  SQLite schema + migrations + single-flight lock + WAL · per-shelf baseline, quota, retry,
  ignore, bounded recheck · secrets-at-rest + redaction + transport/egress hardening · startup
  guard · cron + `--dry-run`/`--apply` · Unraid template.
- **Phase 2:** embedded GUI (session auth + CSRF + write-only secrets + headers) · Hardcover
  source · advanced automation (per-shelf audiobook) · contribute the Goodreads reader to
  `printing-press-library`.

## 18. Open Questions — all resolved by the audit

1. **confidence** = corroboration score → resolver uses self-similarity; confidence is a
   tiebreaker only (§4.1, §8). ✅
2. **HTML pagination** = `per_page=100&print=true&page=N`, sentinel termination, **requires a
   cookie now** even for public profiles (§4.2, §7). ✅
3. **langdetect** = lingua-go high-accuracy, en+es, thresholds 0.65/0.15, title-only, omit
   when unsure (§8). ✅
4. **ebook format** = epub/pdf from direct sources only, best-effort, one source per request,
   Anna's needs FlareSolverr, Spanish handled; send Spanish-edition title; rely on
   reconciliation for failures (§4.3). ✅
