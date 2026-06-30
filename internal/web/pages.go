package web

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/store"
)

func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	state := r.URL.Query().Get("state")
	q := r.URL.Query().Get("q")
	rows, _ := s.st.ListBooks(ctx, state, q, 1000)
	counts, _ := s.st.StateCounts(ctx)
	s.render(w, r, "queue", "Queue", map[string]any{
		"Rows": rows, "State": state, "Q": q, "Counts": counts, "Shown": len(rows),
	})
}

func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	if r.Method == http.MethodPost {
		if !s.localNoSession(r) && !s.requireCSRF(w, r) {
			return
		}
		r.ParseForm()
		src, id := r.PostFormValue("source"), r.PostFormValue("external_id")
		switch r.PostFormValue("action") {
		case "ignore":
			s.st.IgnoreBook(ctx, src, id)
		case "retry":
			s.st.RetryBook(ctx, src, id)
		}
		http.Redirect(w, r, "/review", http.StatusSeeOther)
		return
	}
	nf, _ := s.st.ListBooks(ctx, "not_found", "", 500)
	parked, _ := s.st.ListBooks(ctx, "parked", "", 500)
	s.render(w, r, "review", "Review", map[string]any{"Rows": append(nf, parked...)})
}

func (s *Server) handleShelves(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	cfg := s.cfg()
	if r.Method == http.MethodPost {
		if !s.localNoSession(r) && !s.requireCSRF(w, r) {
			return
		}
		r.ParseForm()
		// Save every shelf row carried in the form (the "shelves" hidden field
		// lists the slugs) so all toggles persist in one click.
		for _, sh := range strings.Split(r.PostFormValue("shelves"), ",") {
			if sh == "" {
				continue
			}
			enabled := r.PostFormValue("enabled_"+sh) != ""
			s.st.SetShelfConfig(ctx, sh, enabled, r.PostFormValue("format_"+sh), r.PostFormValue("language_"+sh))
		}
		http.Redirect(w, r, "/shelves?saved=1", http.StatusSeeOther)
		return
	}
	// Prefer discovered/configured shelves; fall back to the legacy SHELVES text
	// so existing installs still see their shelves until they hit Refresh.
	rows, _ := s.st.AllShelfConfigs(ctx)
	if len(rows) == 0 {
		for _, sh := range cfg.Shelves {
			rows = append(rows, store.ShelfCfg{Shelf: sh, Name: sh, Enabled: true})
		}
	}
	slugs := make([]string, 0, len(rows))
	for _, c := range rows {
		slugs = append(slugs, c.Shelf)
	}
	sourceName := "Goodreads"
	if cfg.Source == "hardcover" {
		sourceName = "Hardcover"
	}
	s.render(w, r, "shelves", "Shelves", map[string]any{
		"Shelves": rows, "Slugs": strings.Join(slugs, ","),
		"CanDiscover": s.discover != nil, "Source": sourceName,
		"Refreshed": r.URL.Query().Get("refreshed"), "Saved": r.URL.Query().Get("saved") != "",
		"Err": r.URL.Query().Get("err"),
	})
}

// handleShelvesRefresh asks the connected source to enumerate the user's shelves
// and records them (disabled by default) so they appear as toggles.
func (s *Server) handleShelvesRefresh(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/shelves", http.StatusSeeOther)
		return
	}
	if !s.localNoSession(r) && !s.requireCSRF(w, r) {
		return
	}
	if s.discover == nil {
		http.Redirect(w, r, "/shelves?err="+url.QueryEscape("shelf discovery is not available"), http.StatusSeeOther)
		return
	}
	shelves, err := s.discover(ctx)
	if err != nil {
		http.Redirect(w, r, "/shelves?err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	for _, sh := range shelves {
		_ = s.st.UpsertDiscoveredShelf(ctx, sh.Slug, sh.Name, sh.Count)
	}
	http.Redirect(w, r, "/shelves?refreshed="+strconv.Itoa(len(shelves)), http.StatusSeeOther)
}
