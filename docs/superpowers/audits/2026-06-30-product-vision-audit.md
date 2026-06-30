# Product-vision audit — BookBridge as a self-hosted Goodreads/Hardcover frontend

**Date:** 2026-06-30
**Why:** The owner clarified the actual product. Everything so far optimised the wrong
center of gravity (a "to-read → Shelfarr download queue with a GUI"). The real product is
different and bigger.

## 1. What this app actually is (restated to confirm understanding)

**A self-hosted alternative frontend to Goodreads/Hardcover — the owner's reading library,
with their own data — that additionally (a) syncs to Shelfarr to download missing ebooks and
(b) cross-references Calibre/CWA to show what they already own.**

The covers / visual polish are secondary. The center is **the owner's stuff**: what they read,
when they read/finished it, their reading progress, their own rating, and their thematic shelves
used as **tags to filter the library**. Shelfarr download is **one feature**, not the point.

## 2. The root conceptual error: shelves are not all the same

The code treats every Goodreads shelf identically (a sync target + a CWA shelf + a tag). But the
owner uses **three different kinds** of shelf:

- **Download trigger** — `to-read`: "I want to obtain/read this" → request via Shelfarr.
- **Reading status** — `read`, `currently-reading`, `did-not-finish`: not topics, they are STATE.
- **Topic tags** — `conocimiento`, `política`, `ciencia`, `ficción`, `medicina`, `salud`,
  `business`, `self-development`, `social`, `aprendizaje`…: categories to **filter** the library.

A book the owner wants to read is filed in `to-read` **and** in its topic shelf(es). Conflating
status / topic / trigger is the source of almost everything that's wrong.

## 3. Audit — current state vs the vision

| Want | Current state | Verdict |
|---|---|---|
| See ALL my books (every shelf) | The catalog only ingests the **sync-enabled** shelves (engine fetches `ShelvesToSync` → only `to-read`). Books only in `read`/`ciencia`/etc. **never enter the DB** | ❌ root gap |
| MY rating (not the crowd average) | `user_rating` is captured, but (a) to-read books are unrated so the UI falls back to `average_rating`, and (b) my rated books (`read`) aren't ingested at all | ❌ |
| When I read it / finished it; dates | `read_at`/`added_at` captured — but only for to-read; no "started"; not surfaced as *my* data | ⚠️ partial |
| Reading progress per book | Goodreads RSS does **not** expose it (confirmed). **Hardcover does** (`user_book_reads`), but the Hardcover source doesn't request it | ❌ (achievable via Hardcover) |
| Filter the Library by topic tags | `book_shelves` stores membership, but the Library filters only by **download state**, never by topic/tag | ❌ |
| Greyed-out / indicator for books I do NOT have in CWA | No cross-reference with Calibre/CWA for "owned / not owned" | ❌ |
| Sync to Shelfarr to download | Works | ✅ (but it's one feature) |
| Topic shelves → CWA tags; status ≠ tag | Currently pushes **every** shelf as a CWA shelf + `gr:` tag, including `to-read`/`read` | ⚠️ mis-modelled |

## 4. The data-model mismatch

- `books.state` conflates the **download lifecycle** (new/requested/downloading/done/not_found…)
  with the idea of "where this book is". There is **no reading-status** concept and no
  ownership concept.
- `book_shelves(source, external_id, shelf)` already holds the tag/membership data — good, it's
  the foundation for tags — but nothing surfaces or filters by it, and topic shelves aren't even
  ingested because the engine only fetches enabled (download) shelves.
- `shelf_config` models shelves as **download toggles**, not as a status/topic classification.
- The Hardcover source fetches only `book{id,title,year,author}` — none of the owner's data
  (rating, status, dates, progress) that its API readily provides.

## 5. Re-architecture — what has to change

1. **Catalog ≠ download queue.** Split two responsibilities: (a) **ingest the whole library**
   (every shelf's books, with tags + reading-status + rating + dates + progress) and (b) mark
   which shelf(es) trigger Shelfarr downloads (`to-read`). Today they're the same fetch.
2. **Model split:** keep `state` as the *download* state; add `reading_status`
   (to_read/reading/read/dnf, derived from status shelves), `started_at`, `progress`, and an
   **owned/in-CWA** representation.
3. **Classify shelves:** status vs topic (configurable; defaults: read / currently-reading /
   to-read / did-not-finish = status, everything else = topic tag).
4. **Library is the center:** filter by **topic tag**, **reading status**, **download state**,
   and **ownership**; grey out books not in CWA; show **my** rating (stars), status, dates,
   progress. Make it the landing experience.
5. **CWA cross-reference:** query the Calibre library and mark each catalog book owned / not.
6. **CWA push, refined:** topic shelves → Calibre tags; reading status → Calibre read/unread (or
   a status column); don't turn `to-read`/`read` into Calibre *shelves*.
7. **Hardcover to vision parity:** request rating + status + dates + progress (one query gives
   all of it), so Hardcover users get the full picture too.

## 6. Phased plan

- **Phase 1 — Full-library ingest.** Fetch ALL shelves (Goodreads) / all `user_books`
  (Hardcover) into the catalog with tags + reading-status + rating + dates. Decouple from the
  Shelfarr download queue (download only the `to-read` trigger shelf).
- **Phase 2 — Library frontend.** Topic-tag + status + download-state + ownership filters; show
  my rating / dates / status; grey out non-owned (CWA cross-reference).
- **Phase 3 — Reading progress** (Hardcover; best-effort Goodreads currently-reading HTML) +
  Hardcover full personal data.
- **Phase 4 — Refined CWA push** (topic=tag, status=read-state).

## 7. Open product decisions (need the owner)

1. **Catalog source of truth:** Goodreads (all shelves; gives rating/status/dates/tags, but no
   progress) as primary with Hardcover only for progress? Or Hardcover as primary (rating +
   status + dates + progress in one query, but the topic shelves live in Goodreads)?
2. **Ownership indicator:** is "in CWA = owned" enough, or should the greyed-out state also
   distinguish downloading / requested / not-found?
