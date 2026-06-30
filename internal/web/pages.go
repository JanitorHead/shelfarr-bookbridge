package web

import (
	"context"
	"net/http"
)

func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	state := r.URL.Query().Get("state")
	rows, _ := s.st.ListBooks(ctx, state, 500)
	s.render(w, r, "queue", "Queue", map[string]any{"Rows": rows, "State": state})
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
	nf, _ := s.st.ListBooks(ctx, "not_found", 500)
	parked, _ := s.st.ListBooks(ctx, "parked", 500)
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
		// A POST carries one shelf row (field "shelf"); save just that row.
		sh := r.PostFormValue("shelf")
		if sh != "" {
			enabled := r.PostFormValue("enabled_"+sh) != ""
			s.st.SetShelfConfig(ctx, sh, enabled, r.PostFormValue("format_"+sh), r.PostFormValue("language_"+sh))
		}
		http.Redirect(w, r, "/shelves", http.StatusSeeOther)
		return
	}
	cfgs, _ := s.st.ShelfConfigs(ctx, cfg.Shelves)
	s.render(w, r, "shelves", "Shelves", map[string]any{"Shelves": cfgs})
}
