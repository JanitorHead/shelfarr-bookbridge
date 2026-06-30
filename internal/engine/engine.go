package engine

import (
	"context"
	"errors"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/resolver"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/shelfarr"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/store"
)

type Engine struct {
	src      sources.Source
	st       *store.Store
	sh       *shelfarr.Client
	cfg      config.Config
	detector LanguageDetector
	logf     func(string, ...any)
}

// SetLogf attaches a logger so the run narrates what it's doing (per book + phase).
func (e *Engine) SetLogf(fn func(string, ...any)) { e.logf = fn }

func (e *Engine) log(format string, a ...any) {
	if e.logf != nil {
		e.logf(format, a...)
	}
}

type Report struct {
	Fetched, New, Requested, NotFound, AlreadyExists int
	Reconciled, Completed, Failed, Rechecked, Parked int
	Errors                                           int // per-item transient failures (timeouts, 5xx) — isolated, not fatal
}

func New(src sources.Source, st *store.Store, sh *shelfarr.Client, cfg config.Config) *Engine {
	return &Engine{src: src, st: st, sh: sh, cfg: cfg}
}

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

var ErrRunInProgress = errors.New("a sync run is already in progress")

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

func (e *Engine) Run(ctx context.Context, dryRun bool) (Report, error) {
	ok, err := e.st.AcquireRun(ctx)
	if err != nil {
		return Report{}, err
	}
	if !ok {
		e.log("a sync run is already in progress — skipping this trigger")
		return Report{}, ErrRunInProgress
	}
	defer e.st.ReleaseRun(ctx)
	e.log("sync started (dryRun=%v)", dryRun)
	var rep Report
	downloadShelves, err := e.st.ShelvesToSync(ctx, e.cfg.Shelves)
	if err != nil {
		return rep, err
	}
	// Catalog = ALL known shelves (the whole library), not just the download
	// targets. Before discovery has run we only know the download shelves.
	catalogShelves, err := e.st.AllShelfSlugs(ctx)
	if err != nil {
		return rep, err
	}
	if len(catalogShelves) == 0 {
		catalogShelves = downloadShelves
	}
	e.log("catalog shelves: %v · download shelves: %v", catalogShelves, downloadShelves)
	books, err := e.src.Fetch(ctx, catalogShelves)
	if err != nil {
		e.log("fetch error: %v", err)
		return rep, err
	}
	rep.Fetched = len(books)
	e.log("fetched %d book(s) from source", len(books))

	newBooks, err := e.st.Diff(ctx, books)
	if err != nil {
		return rep, err
	}
	rep.New = len(newBooks)
	_ = e.st.RefreshReadingStatus(ctx)
	if err := e.st.PromoteDownloadable(ctx, downloadShelves); err != nil {
		return rep, err
	}
	e.log("%d new book(s) added to catalog; download shelves promoted to the queue", len(newBooks))

	// Request from the pending 'new' pool (not just freshly-diffed books) so a
	// backlog larger than one run's quota drains across successive runs.
	pending, err := e.st.PendingNewItems(ctx, e.cfg.MaxRequestsPerRun)
	if err != nil {
		return rep, err
	}
	_ = e.st.BeginProgress(ctx, len(pending))
	e.log("requesting up to %d book(s) this run", len(pending))
	stopped := false
	for i, b := range pending {
		if e.st.StopRequested(ctx) { // cooperative cancel between books
			e.log("stop requested — halting after %d/%d book(s)", i, len(pending))
			stopped = true
			break
		}
		// Surface live progress so the GUI can show "Processing 12/121 …" with the
		// current title and running counters as the request phase advances.
		_ = e.st.SetProgress(ctx, i, b.Title, rep.Requested, rep.NotFound, rep.Errors)
		e.log("[%d/%d] %q by %s", i+1, len(pending), b.Title, b.Author)
		// Query by clean title+author, not ISBN: a Goodreads ISBN often resolves
		// to a different-language edition/work in the metadata providers (e.g. a
		// Spanish ISBN -> the English original), which then fails title matching.
		q := resolver.SearchQuery(b.Title, b.Author)
		results, err := e.sh.Search(ctx, q, 10)
		if err != nil {
			// per-item isolation: one slow/failed search must not abort the run.
			e.log("    Shelfarr search failed: %v", err)
			rep.Errors++
			if !dryRun {
				_ = e.st.SetState(ctx, b, "not_found") // routes into bounded recheck
				_, _ = e.st.IncAttempt(ctx, b.Source, b.ExternalID)
			}
			continue
		}
		pick, _ := resolver.Resolve(b, results, e.cfg.SimilarityThreshold)
		if pick == nil {
			e.log("    no match above threshold among %d result(s) — not found", len(results))
			rep.NotFound++
			if !dryRun {
				_ = e.st.SetState(ctx, b, "not_found")
			}
			continue
		}
		if dryRun {
			e.log("    would request (work %s)", pick.WorkID)
			continue // nothing sent; dry-run requests nothing
		}
		lang := e.detectLang(b)
		if lang != "" {
			_ = e.st.SetChosenLanguage(ctx, b, lang)
		}
		if err := e.st.SetState(ctx, b, "requesting"); err != nil { // intent before POST
			return rep, err
		}
		id, exists, err := e.sh.CreateRequest(ctx, shelfarr.CreateRequestParams{
			WorkID:    pick.WorkID,
			BookTypes: []string{e.formatFor(ctx, b)},
			Language:  lang,
			Title:     b.Title,
			Author:    b.Author,
			CoverURL:  pick.CoverURL,
			Year:      pick.Year,
		})
		if err != nil {
			// per-item isolation: a failed POST must not abort the rest of the run.
			e.log("    Shelfarr request failed: %v", err)
			rep.Errors++
			_ = e.st.SetState(ctx, b, "not_found")
			_, _ = e.st.IncAttempt(ctx, b.Source, b.ExternalID)
			continue
		}
		if exists {
			e.log("    already requested in Shelfarr")
			rep.AlreadyExists++
		} else {
			e.log("    requested ✓")
			rep.Requested++
		}
		_ = e.st.SetRequested(ctx, b, pick.WorkID, id)
	}
	_ = e.st.SetProgress(ctx, len(pending), "", rep.Requested, rep.NotFound, rep.Errors)
	if stopped {
		e.log("stopped: requested=%d not_found=%d errors=%d", rep.Requested, rep.NotFound, rep.Errors)
		return rep, nil // user asked to stop; skip recheck/reconcile
	}
	e.log("request phase done: requested=%d not_found=%d already=%d errors=%d", rep.Requested, rep.NotFound, rep.AlreadyExists, rep.Errors)
	if !dryRun {
		if err := e.recheckPhase(ctx, &rep); err != nil {
			return rep, err
		}
		if err := e.reconcilePhase(ctx, &rep); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

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
	q := resolver.SearchQuery(b.Title, b.Author)
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
	id, exists, err := e.sh.CreateRequest(ctx, shelfarr.CreateRequestParams{
		WorkID: pick.WorkID, BookTypes: []string{e.formatFor(ctx, b)}, Language: lang,
		Title: b.Title, Author: b.Author, CoverURL: pick.CoverURL, Year: pick.Year,
	})
	if err != nil {
		return "", err
	}
	if err := e.st.SetRequested(ctx, b, pick.WorkID, id); err != nil {
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
		if e.st.StopRequested(ctx) {
			return nil
		}
		if rep.Requested+rep.Rechecked >= e.cfg.MaxRequestsPerRun {
			break
		}
		outcome, err := e.resolveAndRequest(ctx, b)
		if err != nil {
			// per-item isolation: count toward attempts, park if exhausted, keep going.
			rep.Errors++
			if n, _ := e.st.IncAttempt(ctx, b.Source, b.ExternalID); n >= maxRecheckAttempts {
				_ = e.st.ApplyStatus(ctx, b.Source, b.ExternalID, "parked")
				rep.Parked++
			}
			continue
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
		if e.st.StopRequested(ctx) {
			return nil
		}
		st, err := e.sh.GetRequest(ctx, ref.RequestID)
		if err != nil {
			if err == shelfarr.ErrRequestNotFound {
				_ = e.st.ApplyStatus(ctx, ref.Source, ref.ExternalID, "cancelled")
				continue
			}
			// per-item isolation: a transient status-poll failure retries next run.
			rep.Errors++
			continue
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
