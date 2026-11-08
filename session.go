package main

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"sync"
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

func setSession(w http.ResponseWriter, s Session) {
	token := newToken()
	sessionsMu.Lock()
	sessions[token] = s
	sessionsMu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   3600,
	})
}

func currentSession(r *http.Request) (Session, bool) {
	c, err := r.Cookie("session")
	if err != nil {
		return Session{}, false
	}
	sessionsMu.RLock()
	s, ok := sessions[c.Value]
	sessionsMu.RUnlock()
	return s, ok
}

func currentUser(r *http.Request) (string, bool) {
	s, ok := currentSession(r)
	if !ok {
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