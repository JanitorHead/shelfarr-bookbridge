# BookBridge UX/UI Redesign — Design Spec

**Date:** 2026-07-01
**Goal:** Re-found the web UI around *your reading* instead of the download pipeline: a coherent, professional, "reading-first" frontend for your Goodreads/Hardcover library, where downloads (Shelfarr) and ownership (Calibre-Web) are supporting features.

## Why (the diagnosis)

The current app is organized around the *machine* (the sync pipeline), not the *reader*. The landing page (Dashboard) shows plumbing (`requesting / searching / downloading / not found`); "Review" is download failures with its own tab; "Shelves" is pipeline configuration with its own tab; the actual point — your books (Library) — is buried as tab 2 and is a flat list; Settings dumps ~20 fields in one ungrouped column with secrets divorced from the fields they belong to (CWA password far from CWA username, Shelfarr token far from Shelfarr URL). Five tabs, no clear home.

## Information architecture — 3 areas

The nav becomes **logo · Biblioteca · Actividad · Ajustes**. Dashboard, Review and Shelves disappear as top-level tabs.

| Area | Route | Purpose |
|---|---|---|
| **Biblioteca** | `/` | Your books — the home. Browse/filter/search your whole reading collection. |
| **Actividad** | `/activity` | The download machinery: run controls + status, pipeline counts, "downloading now", and "needs attention" (failures, retry/ignore). Merges today's Dashboard + Review. |
| **Ajustes** | `/settings` | All configuration, grouped into cards — including the shelf/source config that used to be its own tab. |

Route remap: `/` → Biblioteca (was Dashboard); old Dashboard → `/activity`; `/queue` → 301 to `/`; `/review` → `/activity`; `/shelves` → `/settings#shelves`. The `activePage` helper and nav update accordingly.

## Area 1 — Biblioteca (home)

**Layout:** persistent left **sidebar** (filters) + main content area.

**Sidebar** (all counts shown, links compose like today's chips via `ListLibrary`):
- Search box (title/author).
- **Reading status:** Todos · Leyendo · Pendiente · Leído · Abandonado.
- **Tags** (topic shelves): ciencia, política, … (each with count).
- **Ownership:** Todos · Tengo · No tengo.

**Main content** has a **view toggle ▦ rejilla / ▤ lista**, default **rejilla**, remembered in a `bb_view` cookie (read server-side so there's no flash; toggled by a small GET link, no JS needed).
- **Rejilla (grid):** responsive cover grid. Each tile = cover (or initials placeholder), title, and compact indicators (reading-status pill, progress %, your rating stars, ownership dot). Not-owned tiles are dimmed (greyed), as today.
- **Lista (list):** the current rich rows, polished — small cover, title, author, topic tags, progress bar, dates, status + ownership badges.

**Book detail — slide-in drawer.** Clicking a book opens a right-side **drawer** (no full reload): large cover, title/author/year, your rating, reading status + progress, dates, topic tags, ownership, description, and actions (Request / Retry / Ignore / Mark read where applicable) plus external links (Goodreads, and the Calibre book if owned). Implemented CSP-safely: `app.js` intercepts card clicks, `fetch()`es `/book/<source>/<externalID>` (a server-rendered HTML partial), injects it into a drawer element, and shows it; actions inside are normal `<form>` POSTs to existing endpoints. A no-JS fallback: the card is a real link to `/book/<source>/<externalID>` which also renders standalone.

**Covers (fix).** Today `cover_url` is empty for most books, so a grid would look broken. Fixes:
1. Capture reliably at the source — Goodreads `book_large_image_url` (RSS) / the review page, and Hardcover `image.url` (already captured). Investigate why the HTML (`print=true`) path yields no/low-res covers and add a real cover source (RSS large image, or the book page `og:image`).
2. A one-time **backfill pass** to populate covers for the existing catalog.
3. Graceful placeholder (initials over a gradient) when truly absent.

## Area 2 — Actividad

One page merging Dashboard + Review:
- **Run header:** status (Idle / Running with live progress via `app.js` polling `/actions/status`), buttons **Sync now / Dry run / Stop**, and next scheduled run.
- **Pipeline counters:** clickable cells (catalog, new, requested, searching, downloading, done, not_found, …) → deep-link into a filtered view.
- **Downloading now:** compact list.
- **Needs attention:** not_found / parked, each with **Retry / Ignore** (what `/review` does today). The `/review` POST actions stay.

## Area 3 — Ajustes

One page, **sections rendered as cards**, each secret next to the field it belongs to:
1. **Fuente** — Book source; Goodreads (mode + help, user id, cookie, feed key); Hardcover (username, token).
2. **Shelfarr — descargas** — URL, token, allow-insecure, format, max per run, similarity threshold, first-run mode, infer-language.
3. **Estanterías** — the shelf toggles (grouped **status shelves** vs **topic shelves**) with per-shelf format/language and "Refresh from <source>". Folded in from the old Shelves tab; anchored at `#shelves`.
4. **Calibre-Web (CWA)** — enabled, URL, username, **password (adjacent)**, date column.
5. **Horario** — the visual schedule control.
6. **Acceso & UI** — login method, when-required, web UI port.

The POST handler iterates a flattened field list (secrets only saved when non-blank), preserving today's canonical checkbox/select write behavior. Schedule keeps its `composeSchedule` handling.

## Visual system — theme

**Light / Dark / System**, default **System**, with a header toggle.
- Both palettes are CSS-variable tokens. Default (no choice) follows `@media (prefers-color-scheme)`.
- A manual choice persists in a `bb_theme` cookie (values: `system` | `light` | `dark`), read **server-side** and emitted as `<html data-theme="…">` so there is **no flash** and **no inline script** (CSP-safe). The toggle is a small GET form cycling the value.
- Tightened tokens for spacing, typography, cards, badges, form controls, the sidebar, and responsive breakpoints — applied consistently across all three areas.

## Constraints / non-regression invariants

- **Strict CSP** stays (`default-src 'self'; img-src 'self' https: data:`): no inline styles/scripts. Theme via cookie+attribute; view toggle via cookie; drawer/polling via the vendored `/static/app.js`.
- No SQLite WAL; distroless root image; daemon/GUI start without config; store clears stale run lock; checkbox/select write canonical values; catalog ingest never auto-downloads non-trigger shelves.
- All existing store/engine logic is reused — this is a **presentation-layer** redesign plus a covers fix and a new read-only book-detail partial. **No schema change required:** the cover backfill simply fills empty `cover_url` rows from the source (a one-time pass run on the next sync and via a manual "refresh covers" action), so it is naturally idempotent.

## Phasing (each phase ships + is screenshot-verified)

1. **Shell + IA:** 3-area nav, route remap, theme system (light/dark/system), shared visual tokens.
2. **Biblioteca:** sidebar filters + grid/list toggle + polished cards; covers fix + backfill.
3. **Book detail drawer.**
4. **Actividad:** merge Dashboard + Review.
5. **Ajustes:** grouped sections + Shelves folded in.

## Out of scope (now)

Shelfarr download-method fallback (separate feature — needs Shelfarr API investigation), arm64 image, multi-user.
