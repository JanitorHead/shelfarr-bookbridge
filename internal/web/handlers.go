package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/auth"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/scheduler"
)

func itoa(n int) string     { return strconv.Itoa(n) }
func ftoa(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }
func btoa(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
func onoff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// settingFields are the non-secret settings editable in the GUI (env-var keys).
var settingFields = []struct{ Key, Label, Kind string }{
	{"SHELFARR_URL", "Shelfarr URL", "text"},
	{"GOODREADS_USER_ID", "Goodreads user id", "text"},
	{"GOODREADS_VISIBILITY", "Goodreads visibility (public/private)", "text"},
	{"SHELVES", "Shelves (comma-separated)", "text"},
	{"FORMAT", "Format (ebook/audiobook)", "text"},
	{"SCHEDULE", "Schedule (cron)", "text"},
	{"MAX_REQUESTS_PER_RUN", "Max requests per run", "text"},
	{"SIMILARITY_THRESHOLD", "Similarity threshold (0-1)", "text"},
	{"FIRST_RUN", "First run (baseline/backfill)", "text"},
	{"LANG_INFERENCE", "Language inference (on/off)", "text"},
	{"SHELFARR_INSECURE", "Allow http to non-loopback Shelfarr (true/false)", "text"},
	{"GUI_PORT", "GUI port", "text"},
	{"AUTH_METHOD", "Auth method (forms/none)", "text"},
	{"AUTH_REQUIRED", "Auth required (enabled/local)", "text"},
}

var secretFields = []struct{ Key, Label string }{
	{"SHELFARR_TOKEN", "Shelfarr API token"},
	{"GOODREADS_COOKIE", "Goodreads session cookie"},
	{"GOODREADS_FEED_KEY", "Goodreads RSS feed key"},
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	if r.Method == http.MethodPost {
		if !s.localNoSession(r) && !s.requireCSRF(w, r) {
			return
		}
		r.ParseForm()
		for _, f := range settingFields {
			if v := r.PostFormValue(f.Key); v != "" {
				s.st.SetSetting(ctx, f.Key, v)
			}
		}
		for _, f := range secretFields {
			if v := r.PostFormValue(f.Key); v != "" { // only overwrite when provided
				s.st.SetSetting(ctx, f.Key, v)
			}
		}
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}
	cfg := s.cfg()
	type field struct{ Key, Label, Value string }
	var fields []field
	cur := map[string]string{
		"SHELFARR_URL": cfg.ShelfarrURL, "GOODREADS_USER_ID": cfg.GoodreadsUserID,
		"GOODREADS_VISIBILITY": s.settingValue("GOODREADS_VISIBILITY"), "SHELVES": strings.Join(cfg.Shelves, ","),
		"FORMAT": cfg.Format, "SCHEDULE": cfg.Schedule, "MAX_REQUESTS_PER_RUN": itoa(cfg.MaxRequestsPerRun),
		"SIMILARITY_THRESHOLD": ftoa(cfg.SimilarityThreshold), "FIRST_RUN": cfg.FirstRun,
		"LANG_INFERENCE": onoff(cfg.LangInference), "SHELFARR_INSECURE": btoa(cfg.ShelfarrInsecure),
		"GUI_PORT": cfg.GUIPort, "AUTH_METHOD": cfg.AuthMethod, "AUTH_REQUIRED": cfg.AuthRequired,
	}
	for _, f := range settingFields {
		fields = append(fields, field{f.Key, f.Label, cur[f.Key]})
	}
	type secret struct {
		Key, Label string
		Set        bool
	}
	var secrets []secret
	for _, f := range secretFields {
		secrets = append(secrets, secret{f.Key, f.Label, s.settingValue(f.Key) != ""})
	}
	s.render(w, r, "settings", "Settings", map[string]any{"Fields": fields, "Secrets": secrets})
}

func (s *Server) localNoSession(r *http.Request) bool {
	return s.session(r) == nil && auth.IsLocalAddr(r.RemoteAddr)
}

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
	running, startedAt, _ := s.st.RunState(ctx)
	last, hasLast, _ := s.st.LatestRun(ctx)
	recent, _ := s.st.RecentRuns(ctx, 5)
	var next time.Time
	if sched := s.cfg().Schedule; sched != "" {
		next, _ = scheduler.Next(sched, time.Now())
	}
	needsAuth := s.settingValue("GOODREADS_COOKIE") == "" && s.settingValue("GOODREADS_FEED_KEY") == ""
	started := r.URL.Query().Get("started") != ""
	s.render(w, r, "dashboard", "Dashboard", map[string]any{
		"Cells": cells, "NeedsAuth": needsAuth, "Started": started,
		"Running": running, "StartedAt": startedAt,
		"Last": last, "HasLast": hasLast, "Recent": recent, "NextRun": next,
	})
}

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if !s.localNoSession(r) && !s.requireCSRF(w, r) {
		return
	}
	r.ParseForm()
	dryRun := r.PostFormValue("mode") == "dryrun"
	// Kick the run off in the background so the request returns immediately; the
	// browser polls /actions/status to follow progress. runOnce/AcquireRun already
	// serialize, so a collision returns ErrRunInProgress harmlessly in the goroutine.
	// Run outcomes are persisted at the runOnce choke point (R2) and surfaced via
	// RunState/LatestRun on the dashboard (R3); nothing to record here.
	go func() { _, _ = s.run(dryRun) }()
	http.Redirect(w, r, "/?started=1", http.StatusSeeOther)
}

// handleStatus serves the current run state as JSON for app.js polling.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	running, startedAt, _ := s.st.RunState(ctx)
	last, hasLast, _ := s.st.LatestRun(ctx)
	type lastRun struct {
		At      string `json:"at"`
		Mode    string `json:"mode"`
		OK      bool   `json:"ok"`
		Summary string `json:"summary"`
	}
	resp := struct {
		Running   bool     `json:"running"`
		StartedAt string   `json:"startedAt"`
		LastRun   *lastRun `json:"lastRun"`
	}{Running: running}
	if running && !startedAt.IsZero() {
		resp.StartedAt = startedAt.UTC().Format(time.RFC3339)
	}
	if hasLast {
		resp.LastRun = &lastRun{
			At:      last.StartedAt.UTC().Format(time.RFC3339),
			Mode:    last.Mode,
			OK:      last.OK,
			Summary: last.Summary,
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
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
