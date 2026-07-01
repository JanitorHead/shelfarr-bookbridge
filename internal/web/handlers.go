package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
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

// setField is one editable setting. Secret fields are write-only (only saved
// when non-blank) and render as password/textarea inputs with a set/not-set hint.
type setField struct {
	Key, Label, Kind, Help string
	Options                []string
	Secret                 bool
	OnValue, OffValue      string // checkbox canonical values
}

// settingsSection groups related settings into one card. Fields live with the
// thing they configure — notably each secret sits next to its own field (CWA
// password after CWA username, Shelfarr token after the URL).
type settingsSection struct {
	Title, Desc string
	Fields      []setField
}

var settingsSections = []settingsSection{
	{
		Title: "Library source", Desc: "Where BookBridge reads your books and reading data from.",
		Fields: []setField{
			{Key: "SOURCE", Label: "Book source", Kind: "select", Options: []string{"goodreads", "hardcover"}},
			{Key: "GOODREADS_MODE", Label: "Goodreads mode", Kind: "select", Options: []string{"", "private_cookie", "public_rss"},
				Help: "private (cookie) reads private / >100-book shelves and reading progress — recommended. public (RSS) only works for public shelves, max 100."},
			{Key: "GOODREADS_USER_ID", Label: "Goodreads user ID", Kind: "text", Help: "The number in your Goodreads profile URL."},
			{Key: "GOODREADS_COOKIE", Label: "Goodreads session cookie", Kind: "textarea", Secret: true},
			{Key: "GOODREADS_FEED_KEY", Label: "Goodreads RSS feed key", Kind: "password", Secret: true, Help: "Only for public-RSS mode."},
			{Key: "HARDCOVER_USERNAME", Label: "Hardcover username", Kind: "text", Help: "Only if you use Hardcover as the source."},
			{Key: "HARDCOVER_TOKEN", Label: "Hardcover API token", Kind: "password", Secret: true},
		},
	},
	{
		Title: "Shelfarr — downloads", Desc: "Where the books you want are requested for download.",
		Fields: []setField{
			{Key: "SHELFARR_URL", Label: "Shelfarr URL", Kind: "text", Help: "e.g. http://192.168.1.10:5056"},
			{Key: "SHELFARR_TOKEN", Label: "Shelfarr API token", Kind: "password", Secret: true, Help: "shf_… token with search:read, requests:read, requests:write."},
			{Key: "SHELFARR_INSECURE", Label: "Allow plain HTTP on the LAN", Kind: "checkbox", OnValue: "true", OffValue: "false"},
			{Key: "FORMAT", Label: "Default format", Kind: "select", Options: []string{"ebook", "audiobook"}},
			{Key: "MAX_REQUESTS_PER_RUN", Label: "Max requests per run", Kind: "number"},
			{Key: "SIMILARITY_THRESHOLD", Label: "Match similarity threshold", Kind: "number", Help: "0–1; higher = stricter title/author match."},
			{Key: "FIRST_RUN", Label: "First-run mode", Kind: "select", Options: []string{"baseline", "backfill"},
				Help: "baseline = mark existing books as seen; backfill = request them all."},
			{Key: "LANG_INFERENCE", Label: "Infer language from the title", Kind: "checkbox", OnValue: "on", OffValue: "off",
				Help: "A per-shelf language below overrides this."},
		},
	},
	{
		Title: "Calibre-Web (CWA)", Desc: "Cross-reference what you own and organize your Calibre library.",
		Fields: []setField{
			{Key: "CWA_ENABLED", Label: "Sync to Calibre-Web-Automated", Kind: "checkbox", OnValue: "true", OffValue: "false",
				Help: "Topic shelves → tags, reading lists → shelves, read → read flag, plus rating + date."},
			{Key: "CWA_URL", Label: "CWA URL", Kind: "text", Help: "e.g. http://192.168.1.10:8083"},
			{Key: "CWA_USERNAME", Label: "CWA username", Kind: "text"},
			{Key: "CWA_PASSWORD", Label: "CWA password", Kind: "password", Secret: true},
			{Key: "CWA_DATE_COLUMN", Label: "Date-added custom column id", Kind: "text", Help: "Optional, e.g. 1 — pushes your Goodreads date added."},
		},
	},
	{Title: "Schedule", Desc: "How often BookBridge syncs automatically."}, // rendered specially
	{
		Title: "Access & web UI", Desc: "Who can reach this interface, and on what port.",
		Fields: []setField{
			{Key: "AUTH_METHOD", Label: "Login method", Kind: "select", Options: []string{"forms", "none"}},
			{Key: "AUTH_REQUIRED", Label: "When is login required", Kind: "select", Options: []string{"enabled", "local"},
				Help: "local = no login on your LAN, login from outside; enabled = always."},
			{Key: "GUI_PORT", Label: "Web UI port", Kind: "number", Help: "Changing it needs a container restart."},
		},
	},
}

// allSettingFields flattens every editable field (skipping the Schedule marker).
func allSettingFields() []setField {
	var out []setField
	for _, s := range settingsSections {
		out = append(out, s.Fields...)
	}
	return out
}

// scheduleOption is one entry in the visual schedule preset selector.
type scheduleOption struct{ Value, Label string }

// schedulePresets are the seven visual schedule choices that replace raw cron.
var schedulePresets = []scheduleOption{
	{"off", "Off (no automatic runs)"},
	{"15min", "Every 15 minutes"},
	{"30min", "Every 30 minutes"},
	{"hourly", "Hourly"},
	{"6h", "Every 6 hours"},
	{"daily", "Daily at a set time"},
	{"advanced", "Advanced (raw cron)"},
}

// scheduleVM is the view-model for the visual schedule control on the settings
// page (preset selector + daily time + advanced raw cron).
type scheduleVM struct {
	Preset, Time, Raw, Next, Error string
	Presets                        []scheduleOption
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	if r.Method == http.MethodPost {
		if !s.localNoSession(r) && !s.requireCSRF(w, r) {
			return
		}
		r.ParseForm()
		// Visual schedule: compose the cron from preset/time/raw before the
		// generic loop (SCHEDULE is no longer a generic field). On a bad
		// advanced expression, re-render with an error and save nothing.
		preset := r.PostFormValue("SCHEDULE_PRESET")
		timeHHMM := r.PostFormValue("SCHEDULE_TIME")
		raw := r.PostFormValue("SCHEDULE_RAW")
		cronExpr, err := composeSchedule(preset, timeHHMM, raw)
		if err != nil {
			s.renderSettings(w, r, scheduleVM{
				Preset: preset, Time: timeHHMM, Raw: raw,
				Error: "Invalid schedule: " + err.Error(), Presets: schedulePresets,
			})
			return
		}
		s.st.SetSetting(ctx, "SCHEDULE", cronExpr)
		for _, f := range allSettingFields() {
			switch f.Kind {
			case "checkbox":
				// An unchecked checkbox submits nothing; always write a canonical
				// on/off value so a setting can actually be turned OFF.
				if _, ok := r.PostForm[f.Key]; ok {
					s.st.SetSetting(ctx, f.Key, f.OnValue)
				} else {
					s.st.SetSetting(ctx, f.Key, f.OffValue)
				}
			case "select":
				// A select always submits a deliberate value (incl. the empty
				// "auto" choice), so write it verbatim.
				if _, ok := r.PostForm[f.Key]; ok {
					s.st.SetSetting(ctx, f.Key, r.PostFormValue(f.Key))
				}
			default: // text, number, password, textarea — secrets only save when non-blank
				if v := r.PostFormValue(f.Key); v != "" {
					s.st.SetSetting(ctx, f.Key, v)
				}
			}
		}
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}
	cfg := s.cfg()
	preset, timeHHMM := cronToPreset(cfg.Schedule)
	raw := ""
	if preset == "advanced" {
		raw = cfg.Schedule
	}
	next := ""
	if cfg.Schedule != "" {
		if n, err := scheduler.Next(cfg.Schedule, time.Now()); err == nil {
			next = n.Format("2006-01-02 15:04")
		}
	}
	s.renderSettings(w, r, scheduleVM{
		Preset: preset, Time: timeHHMM, Raw: raw, Next: next, Presets: schedulePresets,
	})
}

// renderSettings renders the grouped settings page. Shared by the GET path and
// the POST error path (invalid schedule).
func (s *Server) renderSettings(w http.ResponseWriter, r *http.Request, sched scheduleVM) {
	cfg := s.cfg()
	cur := map[string]string{
		"SOURCE": cfg.Source, "GOODREADS_MODE": cfg.GoodreadsMode, "SHELFARR_URL": cfg.ShelfarrURL,
		"GOODREADS_USER_ID": cfg.GoodreadsUserID, "HARDCOVER_USERNAME": cfg.HardcoverUsername,
		"FORMAT": cfg.Format, "MAX_REQUESTS_PER_RUN": itoa(cfg.MaxRequestsPerRun),
		"SIMILARITY_THRESHOLD": ftoa(cfg.SimilarityThreshold), "FIRST_RUN": cfg.FirstRun,
		"LANG_INFERENCE": onoff(cfg.LangInference), "SHELFARR_INSECURE": btoa(cfg.ShelfarrInsecure),
		"GUI_PORT": cfg.GUIPort, "AUTH_METHOD": cfg.AuthMethod, "AUTH_REQUIRED": cfg.AuthRequired,
		"CWA_URL": cfg.CWAURL, "CWA_USERNAME": cfg.CWAUsername, "CWA_DATE_COLUMN": cfg.CWADateColumn,
	}
	checked := map[string]bool{
		"LANG_INFERENCE": cfg.LangInference, "SHELFARR_INSECURE": cfg.ShelfarrInsecure, "CWA_ENABLED": cfg.CWAEnabled,
	}
	type vmField struct {
		Key, Label, Kind, Value, Help string
		Options                       []string
		Checked, Secret, Set          bool
	}
	type vmSection struct {
		Title, Desc string
		Fields      []vmField
	}
	var sections []vmSection
	for _, sec := range settingsSections {
		vs := vmSection{Title: sec.Title, Desc: sec.Desc}
		for _, f := range sec.Fields {
			vs.Fields = append(vs.Fields, vmField{
				Key: f.Key, Label: f.Label, Kind: f.Kind, Help: f.Help, Options: f.Options,
				Value: cur[f.Key], Checked: checked[f.Key], Secret: f.Secret,
				Set: f.Secret && s.settingValue(f.Key) != "",
			})
		}
		sections = append(sections, vs)
	}
	s.render(w, r, "settings", "Settings", map[string]any{
		"Sections": sections, "Schedule": sched, "Shelves": s.shelvesVM(r),
	})
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

func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	counts, _ := s.st.StateCounts(ctx)
	order := []string{"catalog", "new", "requesting", "requested", "searching", "downloading", "done", "not_found", "failed", "parked", "cancelled", "baseline", "ignored"}
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
	stopping := r.URL.Query().Get("stopping") != ""
	downloading, _ := s.st.ListBooks(ctx, "downloading", "", 12)
	nf, _ := s.st.ListBooks(ctx, "not_found", "", 500)
	parked, _ := s.st.ListBooks(ctx, "parked", "", 500)
	attention := append(nf, parked...)
	prog, _ := s.st.Progress(ctx)
	s.render(w, r, "dashboard", "Activity", map[string]any{
		"Cells": cells, "NeedsAuth": needsAuth, "Started": started, "Stopping": stopping,
		"Running": running, "StartedAt": startedAt,
		"Last": last, "HasLast": hasLast, "Recent": recent, "NextRun": next,
		"Downloading": downloading, "Attention": attention,
		"TotalBooks": total(counts), "Prog": prog,
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

// handleStop asks the in-flight run to stop at the next safe point (between books).
func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if !s.localNoSession(r) && !s.requireCSRF(w, r) {
		return
	}
	_ = s.st.RequestStop(context.Background())
	http.Redirect(w, r, "/?stopping=1", http.StatusSeeOther)
}

// handleStatus serves the current run state as JSON for app.js polling.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx := context.Background()
	running, startedAt, _ := s.st.RunState(ctx)
	last, hasLast, _ := s.st.LatestRun(ctx)
	prog, _ := s.st.Progress(ctx)
	type lastRun struct {
		At      string `json:"at"`
		Mode    string `json:"mode"`
		OK      bool   `json:"ok"`
		Summary string `json:"summary"`
	}
	type progress struct {
		Total     int    `json:"total"`
		Done      int    `json:"done"`
		Current   string `json:"current"`
		Requested int    `json:"requested"`
		NotFound  int    `json:"notFound"`
		Failed    int    `json:"failed"`
	}
	resp := struct {
		Running   bool      `json:"running"`
		StartedAt string    `json:"startedAt"`
		LastRun   *lastRun  `json:"lastRun"`
		Progress  *progress `json:"progress"`
	}{Running: running}
	if running && !startedAt.IsZero() {
		resp.StartedAt = startedAt.UTC().Format(time.RFC3339)
	}
	if running {
		resp.Progress = &progress{
			Total: prog.Total, Done: prog.Done, Current: prog.Current,
			Requested: prog.Requested, NotFound: prog.NotFound, Failed: prog.Failed,
		}
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
