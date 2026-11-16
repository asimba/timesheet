package main

import (
	"crypto/subtle"
	"net/http"
	"time"
)

const csrfFormField = "csrf_token"
const csrfHeaderName = "X-CSRF-Token"

func (a *App) csrfMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		s, ok := currentSession(w, r, a.cfg.SessionTTLMinutes*60)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		token := r.FormValue(csrfFormField)
		if token == "" {
			token = r.Header.Get(csrfHeaderName)
		}
		if !constantTimeEqual(token, s.CSRFToken) {
			auditFail(r, s.User, "csrf", "request", "invalid_token")
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func (a *App) csrfTokenForTemplate(r *http.Request) string {
	if r == nil {
		return ""
	}
	c, err := r.Cookie("session")
	if err != nil {
		return ""
	}
	sessionsMu.RLock()
	s, ok := sessions[c.Value]
	sessionsMu.RUnlock()
	if !ok || time.Now().After(s.ExpiresAt) {
		return ""
	}
	return s.CSRFToken
}