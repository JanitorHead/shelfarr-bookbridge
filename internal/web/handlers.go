package web

import (
	"context"
	"net/http"
	"time"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/auth"
)

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	if r.Method == http.MethodPost {
		r.ParseForm()
		user := r.PostFormValue("username")
		pass := r.PostFormValue("password")
		storedUser, _, _ := s.st.GetSetting(ctx, "AUTH_USERNAME")
		hash, hasHash, _ := s.st.GetSetting(ctx, "AUTH_PASSWORD_HASH")
		if storedUser == "" {
			storedUser = "admin"
		}
		ok := false
		if !hasHash || hash == "" {
			// first-run: create the admin from the first submitted credentials
			if user != "" && pass != "" {
				h, _ := auth.Hash(pass)
				s.st.SetSetting(ctx, "AUTH_USERNAME", user)
				s.st.SetSetting(ctx, "AUTH_PASSWORD_HASH", h)
				ok = true
				storedUser = user
			}
		} else {
			ok = user == storedUser && auth.Verify(hash, pass)
		}
		if !ok {
			s.render(w, r, "login", "Login", map[string]any{"Error": "Invalid credentials"})
			return
		}
		id, _ := s.newSession(storedUser)
		http.SetCookie(w, &http.Cookie{Name: "bb_session", Value: id, Path: "/", HttpOnly: true,
			SameSite: http.SameSiteStrictMode, Expires: time.Now().Add(7 * 24 * time.Hour)})
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.render(w, r, "login", "Login", nil)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("bb_session"); err == nil {
		s.mu.Lock()
		delete(s.sess, c.Value)
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "bb_session", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	counts, _ := s.st.StateCounts(ctx)
	order := []string{"new", "requesting", "requested", "searching", "downloading", "done", "not_found", "failed", "parked", "cancelled", "baseline", "ignored"}
	type cell struct {
		Name string
		N    int
	}
	var cells []cell
	for _, k := range order {
		if n, ok := counts[k]; ok {
			cells = append(cells, cell{k, n})
		}
	}
	lastRun, _, _ := s.st.GetSetting(ctx, "LAST_RUN")
	cookie, _, _ := s.st.GetSetting(ctx, "GOODREADS_COOKIE")
	feed, _, _ := s.st.GetSetting(ctx, "GOODREADS_FEED_KEY")
	s.render(w, r, "dashboard", "Dashboard", map[string]any{
		"Cells": cells, "LastRun": lastRun,
		"NeedsAuth": cookie == "" && feed == "",
	})
}

// requireCSRF returns false (and writes 403) if the POST lacks a valid token.
func (s *Server) requireCSRF(w http.ResponseWriter, r *http.Request) bool {
	se := s.session(r)
	if se == nil || r.PostFormValue("csrf") != se.csrf {
		http.Error(w, "bad csrf token", http.StatusForbidden)
		return false
	}
	return true
}
