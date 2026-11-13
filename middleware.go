package main

import (
	"net/http"
)

func (a *App) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := currentSession(w, r, a.cfg.SessionTTLMinutes*60); !ok {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

func (a *App) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, ok := currentSession(w, r, a.cfg.SessionTTLMinutes*60)
		if !ok {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		if s.Role != RoleAdmin {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}