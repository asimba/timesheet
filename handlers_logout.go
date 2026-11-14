package main

import (
	"net/http"
)

func (a *App) logoutHandler(w http.ResponseWriter, r *http.Request) {
	if user, ok := currentUser(r); ok {
		auditOK(r, user, "logout", "session")
		if err := forgetUserDevices(a.db, user); err != nil {
			auditFailF(r, user, "logout_all_devices", "devices", "db_error: %v", err)
		} else {
			auditOK(r, user, "logout_all_devices", "devices")
		}
	}
	destroySession(w, r)
	http.SetCookie(w, &http.Cookie{
		Name:     deviceCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}