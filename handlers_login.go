package main

import (
	"errors"
	"log"
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

func (a *App) loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if s, ok := currentSession(r); ok {
			redirectByRole(w, r, s.Role)
			return
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
		auditFail(username, "login", "session", "empty_credentials")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		a.tmpl.Login.Execute(w, LoginData{
			Error:               "Заполните все поля",
			WorkEntriesEditDays: a.cfg.WorkEntriesEditDays,
		})
		return
	}

	for _, admin := range a.cfg.Admins {
		if username == admin.User && password == admin.Password {
			setSession(w, Session{User: admin.User, Role: RoleAdmin})
			auditOK(admin.User, "login", "session role=admin")
			http.Redirect(w, r, "/config", http.StatusSeeOther)
			return
		}
	}

	emp, err := getEmployeeByLogin(a.db, username)
	if err != nil {
		if errors.Is(err, errEmployeeNotFound) {
			auditFail(username, "login", "session", "invalid_credentials")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			a.tmpl.Login.Execute(w, LoginData{
				Error:               "Неверное имя пользователя или пароль",
				WorkEntriesEditDays: a.cfg.WorkEntriesEditDays,
			})
			return
		}
		log.Printf("ошибка чтения сотрудника при логине: %v", err)
		auditFailF(username, "login", "session", "db_error: %v", err)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		a.tmpl.Login.Execute(w, LoginData{
			Error:               "Неверное имя пользователя или пароль",
			WorkEntriesEditDays: a.cfg.WorkEntriesEditDays,
		})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(emp.PasswordHash), []byte(password)); err != nil {
		auditFail(username, "login", "session", "invalid_credentials")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		a.tmpl.Login.Execute(w, LoginData{
			Error:               "Неверное имя пользователя или пароль",
			WorkEntriesEditDays: a.cfg.WorkEntriesEditDays,
		})
		return
	}

	setSession(w, Session{User: emp.Login, Role: RoleEmployee, EmployeeID: emp.ID})
	auditOK(emp.Login, "login", "session role=employee")
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