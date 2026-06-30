package web

import "net/http"

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) { s.render(w, r, "settings", "Settings", nil) }
func (s *Server) handleSync(w http.ResponseWriter, r *http.Request)      { http.Redirect(w, r, "/", http.StatusSeeOther) }
