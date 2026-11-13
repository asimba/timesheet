package main

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"sync"
	"time"
)

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleEmployee Role = "employee"
)

type Session struct {
	User       string
	Role       Role
	EmployeeID int64
	ExpiresAt  time.Time
}

var (
	sessions   = make(map[string]Session)
	sessionsMu sync.RWMutex
)

func newToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		log.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func setSession(w http.ResponseWriter, s Session, maxAge int) {
	token := newToken()
	ttl := time.Duration(maxAge) * time.Second
	s.ExpiresAt = time.Now().Add(ttl)
	sessionsMu.Lock()
	sessions[token] = s
	sessionsMu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})
}

func currentSession(w http.ResponseWriter, r *http.Request, maxAge int) (Session, bool) {
	c, err := r.Cookie("session")
	if err != nil {
		return Session{}, false
	}
	sessionsMu.Lock()
	s, ok := sessions[c.Value]
	if ok && time.Now().After(s.ExpiresAt) {
		delete(sessions, c.Value)
		ok = false
	}
	if ok {
		ttl := time.Duration(maxAge) * time.Second
		s.ExpiresAt = time.Now().Add(ttl)
		sessions[c.Value] = s
	}
	sessionsMu.Unlock()
	if !ok {
		return Session{}, false
	}
	log.Printf("[AUDIT] ip=%s user=%s action=extend_session target=session result=ok", clientIP(r), s.User)
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    c.Value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	})
	return s, true
}

func currentUser(r *http.Request) (string, bool) {
	c, err := r.Cookie("session")
	if err != nil {
		return "", false
	}
	sessionsMu.RLock()
	s, ok := sessions[c.Value]
	sessionsMu.RUnlock()
	if !ok || time.Now().After(s.ExpiresAt) {
		return "", false
	}
	return s.User, true
}

func destroySession(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("session"); err == nil {
		sessionsMu.Lock()
		delete(sessions, c.Value)
		sessionsMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}