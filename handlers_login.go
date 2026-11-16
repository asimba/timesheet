package main

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"golang.org/x/crypto/bcrypt"
)

func (a *App) loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if s, ok := currentSession(w, r, a.cfg.SessionTTLMinutes*60); ok {
			redirectByRole(w, r, s.Role)
			return
		}
		deviceID, err := getOrCreateDeviceID(w, r)
		if err != nil {
			log.Printf("не удалось создать device_id: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		login, err := findUserDevice(a.db, deviceID)
		if err == nil && login != "" {
			for _, admin := range a.cfg.Admins {
				if login == admin.User {
					if err := setSession(w, r, Session{User: admin.User, Role: RoleAdmin}, a.cfg.SessionTTLMinutes*60); err != nil {
						log.Printf("не удалось создать сессию: %v", err)
						http.Error(w, "Internal Server Error", http.StatusInternalServerError)
						return
					}
					auditOK(r, admin.User, "login_device", "session role=admin")
					http.Redirect(w, r, "/config", http.StatusSeeOther)
					return
				}
			}
			emp, err := getEmployeeByLogin(a.db, login)
			if err == nil {
				if err := setSession(w, r, Session{User: emp.Login, Role: RoleEmployee, EmployeeID: emp.ID}, a.cfg.SessionTTLMinutes*60); err != nil {
					log.Printf("не удалось создать сессию: %v", err)
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
					return
				}
				auditOK(r, emp.Login, "login_device", "session role=employee")
				http.Redirect(w, r, "/timesheets", http.StatusSeeOther)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := a.tmpl.Login.Execute(w, LoginData{
			WorkEntriesEditDays: a.cfg.WorkEntriesEditDays,
			CSRFToken:           a.csrfTokenForTemplate(r),
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
			CSRFToken:           a.csrfTokenForTemplate(r),
		})
		return
	}
	ip := clientIP(r)
	if blocked, retryAfter := a.login.Check(ip, username); blocked {
		auditFail(r, username, "login", "session", "rate_limited")
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		return
	}
	for _, admin := range a.cfg.Admins {
		if username == admin.User && constantTimeEqual(password, admin.Password) {
			if err := setSession(w, r, Session{User: admin.User, Role: RoleAdmin}, a.cfg.SessionTTLMinutes*60); err != nil {
				log.Printf("не удалось создать сессию: %v", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			deviceID, err := getOrCreateDeviceID(w, r)
			if err != nil {
				log.Printf("не удалось создать device_id: %v", err)
			} else if err := rememberUserDevice(a.db, admin.User, deviceID); err != nil {
				log.Printf("не удалось запомнить устройство: %v", err)
			}
			a.login.Reset(ip, username)
			auditOK(r, admin.User, "login", "session role=admin")
			http.Redirect(w, r, "/config", http.StatusSeeOther)
			return
		}
	}
	emp, err := getEmployeeByLogin(a.db, username)
	if err != nil {
		if errors.Is(err, errEmployeeNotFound) {
			bcrypt.CompareHashAndPassword(dummyBcryptHash, []byte(password))
			a.login.RecordFailure(ip, username)
			auditFail(r, username, "login", "session", "invalid_credentials")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			a.tmpl.Login.Execute(w, LoginData{
				Error:               "Неверное имя пользователя или пароль",
				WorkEntriesEditDays: a.cfg.WorkEntriesEditDays,
				CSRFToken:           a.csrfTokenForTemplate(r),
			})
			return
		}
		log.Printf("ошибка чтения сотрудника при логине: %v", err)
		auditFailF(r, username, "login", "session", "db_error: %v", err)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		a.tmpl.Login.Execute(w, LoginData{
			Error:               "Неверное имя пользователя или пароль",
			WorkEntriesEditDays: a.cfg.WorkEntriesEditDays,
			CSRFToken:           a.csrfTokenForTemplate(r),
		})
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(emp.PasswordHash), []byte(password)); err != nil {
		a.login.RecordFailure(ip, username)
		auditFail(r, username, "login", "session", "invalid_credentials")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		a.tmpl.Login.Execute(w, LoginData{
			Error:               "Неверное имя пользователя или пароль",
			WorkEntriesEditDays: a.cfg.WorkEntriesEditDays,
			CSRFToken:           a.csrfTokenForTemplate(r),
		})
		return
	}
	if err := setSession(w, r, Session{User: emp.Login, Role: RoleEmployee, EmployeeID: emp.ID}, a.cfg.SessionTTLMinutes*60); err != nil {
		log.Printf("не удалось создать сессию: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	deviceID, err := getOrCreateDeviceID(w, r)
	if err != nil {
		log.Printf("не удалось создать device_id: %v", err)
	} else if err := rememberUserDevice(a.db, emp.Login, deviceID); err != nil {
		log.Printf("не удалось запомнить устройство: %v", err)
	}
	a.login.Reset(ip, username)
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

var dummyBcryptHash = []byte("$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")