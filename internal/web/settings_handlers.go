package web

import (
	"net/http"

	"github.com/michael-duren/rubber-duck/internal/auth"
	"github.com/michael-duren/rubber-duck/internal/web/views"
)

func (h *handlers) settingsPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, views.Settings("", ""))
}

func (h *handlers) changePassword(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	current := r.FormValue("current_password")
	newPassword := r.FormValue("new_password")

	hash, err := h.store.PasswordHash(r.Context(), user.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if !auth.CheckPassword(hash, current) {
		h.render(w, r, views.Settings("Current password is wrong.", ""))
		return
	}
	if fail := validatePassword(newPassword); fail != "" {
		h.render(w, r, views.Settings(fail, ""))
		return
	}

	newHash, err := auth.HashPassword(newPassword)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if err := h.store.UpdatePassword(r.Context(), user.ID, newHash); err != nil {
		h.serverError(w, r, err)
		return
	}

	// Keep the session that just made this request; log out every other one.
	var keep []byte
	if c, err := r.Cookie(sessionCookie); err == nil {
		keep = auth.HashToken(c.Value)
	}
	if err := h.store.DeleteOtherSessions(r.Context(), user.ID, keep); err != nil {
		h.serverError(w, r, err)
		return
	}

	h.render(w, r, views.Settings("", "Password changed. Other sessions have been logged out."))
}

func validatePassword(password string) string {
	if len(password) < 8 {
		return "New password must be at least 8 characters."
	}
	return ""
}
