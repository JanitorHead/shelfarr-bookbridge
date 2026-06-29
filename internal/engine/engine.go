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
			continue // nothing sent; dry-run requests nothing
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
