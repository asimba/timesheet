package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
)

func isIP(s string) bool {
	return net.ParseIP(s) != nil
}

func clientIP(r *http.Request) string {
	if r == nil {
		return "unknown"
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for _, part := range strings.Split(xff, ",") {
			candidate := strings.TrimSpace(part)
			if isIP(candidate) {
				return candidate
			}
		}
	}
	if xrip := strings.TrimSpace(r.Header.Get("X-Real-IP")); xrip != "" && isIP(xrip) {
		return xrip
	}
	addr := strings.TrimSpace(r.RemoteAddr)
	if addr == "" {
		return "unknown"
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		if isIP(addr) {
			return addr
		}
		return "unknown"
	}
	if isIP(host) {
		return host
	}
	return "unknown"
}

func auditOK(r *http.Request, user, action, target string) {
	log.Printf("[AUDIT] ip=%s user=%s action=%s target=%s result=ok", clientIP(r), user, action, target)
}

func auditFail(r *http.Request, user, action, target, reason string) {
	log.Printf("[AUDIT] ip=%s user=%s action=%s target=%s result=fail reason=%s", clientIP(r), user, action, target, reason)
}

func auditFailF(r *http.Request, user, action, target, format string, args ...any) {
	auditFail(r, user, action, target, fmt.Sprintf(format, args...))
}

func targetGroup(id int64, name string) string {
	if id == 0 {
		return fmt.Sprintf("group(name=%q)", name)
	}
	return fmt.Sprintf("group(id=%d, name=%q)", id, name)
}

func targetProject(id int64, name, code string) string {
	if id == 0 {
		return fmt.Sprintf("project(name=%q, code=%q)", name, code)
	}
	return fmt.Sprintf("project(id=%d, name=%q, code=%q)", id, name, code)
}

func targetEmployee(id int64, login string) string {
	if id == 0 {
		return fmt.Sprintf("employee(login=%q)", login)
	}
	return fmt.Sprintf("employee(id=%d, login=%q)", id, login)
}

func (a *App) auditOKWorkEntry(r *http.Request, user, action string, id, employeeID, projectID int64, workDate, startTime string, duration int64) {
	log.Printf("[AUDIT] ip=%s user=%s action=%s target=work_entry(id=%d, employee_id=%d, project_id=%d, date=%s, start=%s, duration_min=%d) result=ok",
		clientIP(r), user, action, id, employeeID, projectID, workDate, startTime, duration)
}

func (a *App) auditFailWorkEntry(r *http.Request, user, action, target, reason string) {
	log.Printf("[AUDIT] ip=%s user=%s action=%s target=%s result=fail reason=%s", clientIP(r), user, action, target, reason)
}

func (a *App) auditFailWorkEntryF(r *http.Request, user, action, target, format string, args ...any) {
	log.Printf("[AUDIT] ip=%s user=%s action=%s target=%s result=fail reason=%s", clientIP(r), user, action, target, fmt.Sprintf(format, args...))
}