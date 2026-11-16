package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"context"
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
	IP         string
	UserAgent  string
	CSRFToken  string
}

const deviceCookieName = "device_id"

const extendAuditInterval = 1 * time.Minute

var (
	sessions   = make(map[string]Session)
	sessionsMu sync.RWMutex
)

func newToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func newDeviceID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func newCSRFToken(sessionToken string) (string, error) {
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(sessionToken))
	mac.Write(salt)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func setSession(w http.ResponseWriter, r *http.Request, s Session, maxAge int) error {
	token, err := newToken()
	if err != nil {
		return err
	}
	csrf, err := newCSRFToken(token)
	if err != nil {
		return err
	}
	s.CSRFToken = csrf
	ttl := time.Duration(maxAge) * time.Second
	s.ExpiresAt = time.Now().Add(ttl)
	s.IP = clientIP(r)
	s.UserAgent = r.UserAgent()
	sessionsMu.Lock()
	for tok, existing := range sessions {
		if existing.User == s.User && existing.Role == s.Role {
			delete(sessions, tok)
		}
	}
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
	return nil
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
		if s.IP != "" && clientIP(r) != s.IP {
			delete(sessions, c.Value)
			sessionsMu.Unlock()
			log.Printf("[AUDIT] ip=%s user=%s action=extend_session target=session result=fail reason=ip_changed", clientIP(r), s.User)
			return Session{}, false
		}
		if s.UserAgent != "" && r.UserAgent() != s.UserAgent {
			delete(sessions, c.Value)
			sessionsMu.Unlock()
			log.Printf("[AUDIT] ip=%s user=%s action=extend_session target=session result=fail reason=ua_changed", clientIP(r), s.User)
			return Session{}, false
		}
		now := time.Now()
		shouldAudit := s.ExpiresAt.Sub(now) < time.Duration(maxAge)*time.Second-extendAuditInterval
		s.ExpiresAt = now.Add(time.Duration(maxAge) * time.Second)
		sessions[c.Value] = s
		sessionsMu.Unlock()
		if shouldAudit {
			log.Printf("[AUDIT] ip=%s user=%s action=extend_session target=session result=ok", clientIP(r), s.User)
		}
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
	sessionsMu.Unlock()
	return Session{}, false
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

func getOrCreateDeviceID(w http.ResponseWriter, r *http.Request) (string, error) {
	if c, err := r.Cookie(deviceCookieName); err == nil && c.Value != "" {
		return c.Value, nil
	}
	id, err := newDeviceID()
	if err != nil {
		return "", err
	}
	const deviceTTL = 30 * 24 * 60 * 60
	http.SetCookie(w, &http.Cookie{
		Name:     deviceCookieName,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   deviceTTL,
	})
	return id, nil
}

func activeSessionCountByEmployeeID(employeeID int64) int {
	now := time.Now()
	sessionsMu.RLock()
	defer sessionsMu.RUnlock()
	n := 0
	for _, s := range sessions {
		if s.EmployeeID == employeeID && s.Role == RoleEmployee && s.ExpiresAt.After(now) {
			n++
		}
	}
	return n
}

func killSessionsByEmployeeID(employeeID int64) int {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	n := 0
	for tok, s := range sessions {
		if s.EmployeeID == employeeID && s.Role == RoleEmployee {
			delete(sessions, tok)
			n++
		}
	}
	return n
}

func startSessionJanitor(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				removed := cleanupExpiredSessions()
				if removed > 0 {
					log.Printf("janitor сессий: удалено %d просроченных сессий", removed)
				}
			}
		}
	}()
}

func cleanupExpiredSessions() int {
	now := time.Now()
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	n := 0
	for tok, s := range sessions {
		if now.After(s.ExpiresAt) {
			delete(sessions, tok)
			n++
		}
	}
	return n
}