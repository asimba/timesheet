package main

import (
	"errors"
	"log"
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

func (a *App) loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if s, ok := currentSession(w, r, a.cfg.SessionTTLMinutes*60); ok {
			redirectByRole(w, r, s.Role)
			return
		}
		deviceID := getOrCreateDeviceID(w, r)
		login, err := findUserDevice(a.db, deviceID)
		if err == nil && login != "" {
			for _, admin := range a.cfg.Admins {
				if login == admin.User {
					setSession(w, r, Session{User: admin.User, Role: RoleAdmin}, a.cfg.SessionTTLMinutes*60)
					auditOK(r, admin.User, "login_device", "session role=admin")
					http.Redirect(w, r, "/config", http.StatusSeeOther)
					return
				}
			}
			emp, err := getEmployeeByLogin(a.db, login)
			if err == nil {
				setSession(w, r, Session{User: emp.Login, Role: RoleEmployee, EmployeeID: emp.ID}, a.cfg.SessionTTLMinutes*60)
				auditOK(r, emp.Login, "login_device", "session role=employee")
				http.Redirect(w, r, "/timesheets", http.StatusSeeOther)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := a.tmpl.Login.Execute(w, LoginData{
			WorkEntriesEditDays: a.cfg.WorkEntriesEditDays,
		}); err != nil {
			log.Printf("ошибка рендера login: %v", err)
		}
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")
	if username == "" || password == "" {
		auditFail(r, username, "login", "session", "empty_credentials")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		a.tmpl.Login.Execute(w, LoginData{
			Error:               "Заполните все поля",
			WorkEntriesEditDays: a.cfg.WorkEntriesEditDays,
		})
		return
	}
	for _, admin := range a.cfg.Admins {
		if username == admin.User && password == admin.Password {
			setSession(w, r, Session{User: admin.User, Role: RoleAdmin}, a.cfg.SessionTTLMinutes*60)
			deviceID := getOrCreateDeviceID(w, r)
			if err := rememberUserDevice(a.db, admin.User, deviceID); err != nil {
				log.Printf("не удалось запомнить устройство: %v", err)
			}
			auditOK(r, admin.User, "login", "session role=admin")
			http.Redirect(w, r, "/config", http.StatusSeeOther)
			return
		}
	}
	emp, err := getEmployeeByLogin(a.db, username)
	if err != nil {
		if errors.Is(err, errEmployeeNotFound) {
			auditFail(r, username, "login", "session", "invalid_credentials")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			a.tmpl.Login.Execute(w, LoginData{
				Error:               "Неверное имя пользователя или пароль",
				WorkEntriesEditDays: a.cfg.WorkEntriesEditDays,
			})
			return
		}
		log.Printf("ошибка чтения сотрудника при логине: %v", err)
		auditFailF(r, username, "login", "session", "db_error: %v", err)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		a.tmpl.Login.Execute(w, LoginData{
			Error:               "Неверное имя пользователя или пароль",
			WorkEntriesEditDays: a.cfg.WorkEntriesEditDays,
		})
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(emp.PasswordHash), []byte(password)); err != nil {
		auditFail(r, username, "login", "session", "invalid_credentials")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		a.tmpl.Login.Execute(w, LoginData{
			Error:               "Неверное имя пользователя или пароль",
			WorkEntriesEditDays: a.cfg.WorkEntriesEditDays,
		})
		return
	}
	setSession(w, r, Session{User: emp.Login, Role: RoleEmployee, EmployeeID: emp.ID}, a.cfg.SessionTTLMinutes*60)
	deviceID := getOrCreateDeviceID(w, r)
	if err := rememberUserDevice(a.db, emp.Login, deviceID); err != nil {
		log.Printf("не удалось запомнить устройство: %v", err)
	}
	auditOK(r, emp.Login, "login", "session role=employee")
	http.Redirect(w, r, "/timesheets", http.StatusSeeOther)
}

func redirectByRole(w http.ResponseWriter, r *http.Request, role Role) {
	switch role {
	case RoleAdmin:
		http.Redirect(w, r, "/config", http.StatusSeeOther)
	default:
		http.Redirect(w, r, "/timesheets", http.StatusSeeOther)
	}
}