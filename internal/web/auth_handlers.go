package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/michael-duren/rubber-duck/internal/auth"
	"github.com/michael-duren/rubber-duck/internal/domain"
	"github.com/michael-duren/rubber-duck/internal/web/views"
)

func (h *handlers) signupPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, views.Signup(""))
}

func (h *handlers) signup(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	if fail := validateCredentials(username, password); fail != "" {
		h.render(w, r, views.Signup(fail))
		return
	}

	hash, err := auth.HashPassword(password)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	u, err := h.store.CreateUser(r.Context(), username, hash)
	if errors.Is(err, domain.ErrUsernameTaken) {
		h.render(w, r, views.Signup("That username is taken."))
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if err := h.setSession(w, r, u.ID); err != nil {
		h.serverError(w, r, err)
		return
	}
	// Straight to the catalog: a fresh signup came from the landing page's
	// pitch and is here to pick a course, not to re-read it.
	http.Redirect(w, r, "/courses", http.StatusSeeOther)
}

func (h *handlers) loginPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, views.Login(""))
}

func (h *handlers) login(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	u, hash, err := h.store.UserByUsername(r.Context(), username)
	if errors.Is(err, domain.ErrNotFound) || (err == nil && !auth.CheckPassword(hash, password)) {
		h.render(w, r, views.Login("Wrong username or password."))
		return
	}
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	if err := h.setSession(w, r, u.ID); err != nil {
		h.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/courses", http.StatusSeeOther)
}

func (h *handlers) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err := h.store.DeleteSession(r.Context(), auth.HashToken(c.Value)); err != nil {
			h.logger.Error("delete session", "err", err)
		}
		http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1})
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func validateCredentials(username, password string) string {
	switch {
	case len(username) < 3 || len(username) > 32:
		return "Username must be 3-32 characters."
	default:
		return validatePassword(password)
	}
}
