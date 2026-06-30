package web

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"html/template"
	"net/http"
	"sync"
	"time"

	"github.com/JanitorHead/shelfarr-bookbridge/internal/auth"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/config"
	"github.com/JanitorHead/shelfarr-bookbridge/internal/engine"
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
	st   *store.Store
	run  Runner
	tmpl *template.Template
	mu   sync.Mutex
	sess map[string]*session
}

func New(st *store.Store, run Runner) *Server {
	t := template.Must(template.ParseFS(tmplFS, "templates/*.html"))
	return &Server{st: st, run: run, tmpl: t, sess: map[string]*session{}}
}

func (s *Server) cfg() config.Config {
	all, _ := s.st.AllSettings(context.Background())
	c, _ := config.LoadEffective(emptyEnv, all) // GUI process: env not used for effective reads
	return c
}

func emptyEnv(string) string { return "" }

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
	mux.Handle("/static/", http.FileServer(http.FS(staticFS)))
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/logout", s.handleLogout)
	mux.HandleFunc("/settings", s.guard(s.handleSettings))
	mux.HandleFunc("/actions/sync", s.guard(s.handleSync))
	mux.HandleFunc("/", s.guard(s.handleDashboard))
	return securityHeaders(mux)
}

func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
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
	base := map[string]any{"Title": title, "Authed": se != nil, "CSRF": "", "Flash": ""}
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
	if err := t.ExecuteTemplate(w, "base", base); err != nil {
		http.Error(w, err.Error(), 500)
	}
}
