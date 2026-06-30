package web

import "net/http"

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request)     { s.render(w, r, "login", "Login", nil) }
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request)    { http.Redirect(w, r, "/login", http.StatusSeeOther) }
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) { s.render(w, r, "dashboard", "Dashboard", nil) }
func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request)  { s.render(w, r, "settings", "Settings", nil) }
func (s *Server) handleSync(w http.ResponseWriter, r *http.Request)      { http.Redirect(w, r, "/", http.StatusSeeOther) }
