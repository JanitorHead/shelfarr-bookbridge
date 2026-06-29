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
}

type Report struct {
	Fetched, New, Requested, NotFound, AlreadyExists int
	Reconciled, Completed, Failed, Rechecked, Parked int
	Errors int // per-item transient failures (timeouts, 5xx) — isolated, not fatal
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

func (e *Engine) Run(ctx context.Context, dryRun bool) (Report, error) {
	ok, err := e.st.AcquireRun(ctx)
	if err != nil {
		return Report{}, err
	}
	if !ok {
		return Report{}, ErrRunInProgress
	}
	defer e.st.ReleaseRun(ctx)
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

	// Request from the pending 'new' pool (not just freshly-diffed books) so a
	// backlog larger than one run's quota drains across successive runs.
	pending, err := e.st.PendingNewItems(ctx, e.cfg.MaxRequestsPerRun)
	if err != nil {
		return rep, err
	}
	for _, b := range pending {
		// Query by clean title+author, not ISBN: a Goodreads ISBN often resolves
		// to a different-language edition/work in the metadata providers (e.g. a
		// Spanish ISBN -> the English original), which then fails title matching.
		q := resolver.SearchQuery(b.Title, b.Author)
		results, err := e.sh.Search(ctx, q, 10)
		if err != nil {
			// per-item isolation: one slow/failed search must not abort the run.
			rep.Errors++
			if !dryRun {
				_ = e.st.SetState(ctx, b, "not_found") // routes into bounded recheck
				_, _ = e.st.IncAttempt(ctx, b.Source, b.ExternalID)
			}
			continue
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
			BookTypes: []string{e.cfg.Format},
			Language:  lang,
			Title:     b.Title,
			Author:    b.Author,
			CoverURL:  pick.CoverURL,
			Year:      pick.Year,
		})
		if err != nil {
			// per-item isolation: a failed POST must not abort the rest of the run.
			rep.Errors++
			_ = e.st.SetState(ctx, b, "not_found")
			_, _ = e.st.IncAttempt(ctx, b.Source, b.ExternalID)
			continue
		}
		if exists {
			rep.AlreadyExists++
		} else {
			rep.Requested++
		}
		_ = e.st.SetRequested(ctx, b, pick.WorkID, id)
	}
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
		WorkID: pick.WorkID, BookTypes: []string{e.cfg.Format}, Language: lang,
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
