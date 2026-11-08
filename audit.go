package main

import (
	"fmt"
	"log"
)

func auditOK(user, action, target string) {
	log.Printf("[AUDIT] user=%s action=%s target=%s result=ok", user, action, target)
}

func auditFail(user, action, target, reason string) {
	log.Printf("[AUDIT] user=%s action=%s target=%s result=fail reason=%s", user, action, target, reason)
}

func auditFailF(user, action, target, format string, args ...any) {
	auditFail(user, action, target, fmt.Sprintf(format, args...))
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

func (a *App) auditOKWorkEntry(user, action string, id, employeeID, projectID int64, workDate, startTime string, duration int64) {
	log.Printf("[AUDIT] user=%s action=%s target=work_entry(id=%d, employee_id=%d, project_id=%d, date=%s, start=%s, duration_min=%d) result=ok",
		user, action, id, employeeID, projectID, workDate, startTime, duration)
}

func (a *App) auditFailWorkEntry(user, action, target, reason string) {
	log.Printf("[AUDIT] user=%s action=%s target=%s result=fail reason=%s", user, action, target, reason)
}

func (a *App) auditFailWorkEntryF(user, action, target, format string, args ...any) {
	log.Printf("[AUDIT] user=%s action=%s target=%s result=fail reason=%s", user, action, target, fmt.Sprintf(format, args...))
}