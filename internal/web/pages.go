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
