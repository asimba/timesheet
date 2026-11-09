package main

import "net/http"

func (a *App) logoutHandler(w http.ResponseWriter, r *http.Request) {
	if user, ok := currentUser(r); ok {
		auditOK(r, user, "logout", "session")
	}
	destroySession(w, r)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}