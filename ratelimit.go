package main

import (
	"sync"
	"time"
)

type loginAttempt struct {
	Count       int
	FirstSeen   time.Time
	LastSeen    time.Time
	BlockedTill time.Time
}

type loginRateLimiter struct {
	mu       sync.Mutex
	byIP     map[string]*loginAttempt
	byLogin  map[string]*loginAttempt
	maxPerIP int
	maxPerLogin int
	window   time.Duration
	blockFor time.Duration
}

func newLoginRateLimiter() *loginRateLimiter {
	return &loginRateLimiter{
		byIP:        make(map[string]*loginAttempt),
		byLogin:     make(map[string]*loginAttempt),
		maxPerIP:    10,
		maxPerLogin: 5,
		window:      15 * time.Minute,
		blockFor:    15 * time.Minute,
	}
}

func (l *loginRateLimiter) check(key string, m map[string]*loginAttempt, max int) (blocked bool, retryAfter time.Duration) {
	now := time.Now()
	a, ok := m[key]
	if !ok {
		return false, 0
	}
	if now.Before(a.BlockedTill) {
		return true, a.BlockedTill.Sub(now)
	}
	if now.Sub(a.FirstSeen) > l.window {
		delete(m, key)
		return false, 0
	}
	if a.Count >= max {
		a.BlockedTill = now.Add(l.blockFor)
		return true, l.blockFor
	}
	return false, 0
}

func (l *loginRateLimiter) Check(ip, login string) (blocked bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if b, r := l.check(ip, l.byIP, l.maxPerIP); b {
		return true, r
	}
	if login != "" {
		if b, r := l.check(login, l.byLogin, l.maxPerLogin); b {
			return true, r
		}
	}
	return false, 0
}

func (l *loginRateLimiter) record(key string, m map[string]*loginAttempt) {
	now := time.Now()
	a, ok := m[key]
	if !ok {
		m[key] = &loginAttempt{Count: 1, FirstSeen: now, LastSeen: now}
		return
	}
	if now.Sub(a.FirstSeen) > l.window {
		a.Count = 1
		a.FirstSeen = now
		a.LastSeen = now
		return
	}
	a.Count++
	a.LastSeen = now
}

func (l *loginRateLimiter) RecordFailure(ip, login string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.record(ip, l.byIP)
	if login != "" {
		l.record(login, l.byLogin)
	}
}

func (l *loginRateLimiter) Reset(ip, login string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.byIP, ip)
	if login != "" {
		delete(l.byLogin, login)
	}
}

func (l *loginRateLimiter) cleanup() {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, a := range l.byIP {
		if now.Sub(a.LastSeen) > l.window && now.After(a.BlockedTill) {
			delete(l.byIP, k)
		}
	}
	for k, a := range l.byLogin {
		if now.Sub(a.LastSeen) > l.window && now.After(a.BlockedTill) {
			delete(l.byLogin, k)
		}
	}
}

func (l *loginRateLimiter) startCleanup(stop <-chan struct{}) {
	go func() {
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				l.cleanup()
			}
		}
	}()
}