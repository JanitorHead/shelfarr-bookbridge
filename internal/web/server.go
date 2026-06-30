package web

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"html/template"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/auth"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/engine"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/sources"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/store"
)

//go:embed templates/*.html
var tmplFS embed.FS

//go:embed static/*
var staticFS embed.FS

// Runner builds the engine from effective config and runs it.
type Runner func(dryRun bool) (engine.Report, error)

type session struct {
	user    string
	csrf    string
	expires time.Time
}

type Server struct {
	st         *store.Store
	run        Runner
	discover   func(ctx context.Context) ([]sources.Shelf, error)
	refreshOwn func(ctx context.Context) error
	tmpl       *template.Template
	getenv     func(string) string
	mu         sync.Mutex
	sess       map[string]*session
}

// SetDiscoverer wires the shelf-discovery function (built from effective config
// in main) so the Shelves page can list the source's shelves as toggles.
func (s *Server) SetDiscoverer(fn func(ctx context.Context) ([]sources.Shelf, error)) {
	s.discover = fn
}

// SetOwnershipRefresher wires the CWA-ownership cross-reference (built from
// effective config in main) so the Library can refresh ownership on demand.
func (s *Server) SetOwnershipRefresher(fn func(ctx context.Context) error) {
	s.refreshOwn = fn
}

func New(st *store.Store, run Runner) *Server {
	funcs := template.FuncMap{
		"stateClass":   stateClass,
		"stateLabel":   stateLabel,
		"initials":     initials,
		"list":         list,
		"optLabel":     optLabel,
		"dateOnly":     dateOnly,
		"stars":        stars,
		"ownership":    ownership,
		"ownLabel":     ownLabel,
		"readingLabel": readingLabel,
		"topicTags":    topicTags,
	}
	t := template.Must(template.New("").Funcs(funcs).ParseFS(tmplFS, "templates/*.html"))
	return &Server{st: st, run: run, tmpl: t, getenv: os.Getenv, sess: map[string]*session{}}
}

// cfg returns the effective config: stored settings overriding the environment,
// so values bootstrapped via env (docker-compose) are visible/editable in the GUI.
func (s *Server) cfg() config.Config {
	all, _ := s.st.AllSettings(context.Background())
	c, _ := config.LoadEffective(s.getenv, all)
	return c
}

// settingValue returns the effective value of a raw setting key (store over env).
func (s *Server) settingValue(key string) string {
	if v, ok, _ := s.st.GetSetting(context.Background(), key); ok && v != "" {
		return v
	}
	return s.getenv(key)
}

func token() string {
	b := make([]byte, 24)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *Server) newSession(user string) (string, *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := token()
	se := &session{user: user, csrf: token(), expires: time.Now().Add(7 * 24 * time.Hour)}
	s.sess[id] = se
	return id, se
}

func (s *Server) session(r *http.Request) *session {
	c, err := r.Cookie("bb_session")
	if err != nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	se := s.sess[c.Value]
	if se == nil || time.Now().After(se.expires) {
		return nil
	}
	return se
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	// Embedded static files carry no modtime, so the browser can't revalidate and
	// may serve stale CSS/JS after a container update. Force revalidation.
	staticH := http.FileServer(http.FS(staticFS))
	mux.Handle("/static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		staticH.ServeHTTP(w, r)
	}))
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/logout", s.handleLogout)
	mux.HandleFunc("/settings", s.guard(s.handleSettings))
	mux.HandleFunc("/actions/sync", s.guard(s.handleSync))
	mux.HandleFunc("/actions/stop", s.guard(s.handleStop))
	mux.HandleFunc("/actions/status", s.guard(s.handleStatus))
	mux.HandleFunc("/queue", s.guard(s.handleQueue))
	mux.HandleFunc("/review", s.guard(s.handleReview))
	mux.HandleFunc("/shelves", s.guard(s.handleShelves))
	mux.HandleFunc("/shelves/refresh", s.guard(s.handleShelvesRefresh))
	mux.HandleFunc("/actions/refresh-ownership", s.guard(s.handleRefreshOwnership))
	mux.HandleFunc("/", s.guard(s.handleDashboard))
	return securityHeaders(mux)
}

func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		// Strict by default; allow external book-cover images (Goodreads/Shelfarr/
		// OpenLibrary serve them over https) and data: placeholders. No inline JS/CSS.
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' https: data:")
		h.ServeHTTP(w, r)
	})
}

// guard enforces auth per the *arr model.
func (s *Server) guard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := s.cfg()
		authed := s.session(r) != nil
		if c.AuthMethod == "none" || (c.AuthRequired == "local" && auth.IsLocalAddr(r.RemoteAddr)) {
			h(w, r)
			return
		}
		if !authed {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		h(w, r)
	}
}

// render executes the base template with a page-specific "content" block.
func (s *Server) render(w http.ResponseWriter, r *http.Request, page, title string, data map[string]any) {
	se := s.session(r)
	cfg := s.cfg()
	// The nav must show whenever the page is actually usable — including the *arr
	// "local bypass" and "auth disabled" modes, where there is no session. The old
	// `.Authed = se != nil` hid the entire nav for LAN users (AUTH_REQUIRED=local).
	canUse := se != nil || cfg.AuthMethod == "none" ||
		(cfg.AuthRequired == "local" && auth.IsLocalAddr(r.RemoteAddr))
	base := map[string]any{
		"Title": title, "Authed": canUse, "HasSession": se != nil,
		"Active": activePage(r.URL.Path), "CSRF": "", "Flash": "",
	}
	if se != nil {
		base["CSRF"] = se.csrf
	}
	for k, v := range data {
		base[k] = v
	}
	t, err := s.tmpl.Clone()
	if err == nil {
		_, err = t.ParseFS(tmplFS, "templates/"+page+".html")
	}
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Never let the browser serve a stale GUI after a container update: these
	// pages are dynamic, so always revalidate. (Static assets under /static keep
	// their own default caching.)
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	if err := t.ExecuteTemplate(w, "base", base); err != nil {
		http.Error(w, err.Error(), 500)
	}
}
