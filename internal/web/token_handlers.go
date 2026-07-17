package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/michael-duren/rubber-duck/internal/auth"
	"github.com/michael-duren/rubber-duck/internal/web/views"
)

const maxTokenNameBytes = 64

// createUserToken mints a CLI token and renders it once, inline in the
// profile page — never via redirect, so it never lands in a URL or log.
func (h *handlers) createUserToken(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" || len(name) > maxTokenNameBytes {
		http.Error(w, "token name must be 1-64 characters", http.StatusBadRequest)
		return
	}

	token, hash := auth.NewUserToken()
	if _, err := h.store.CreateUserToken(r.Context(), user.ID, name, hash); err != nil {
		h.serverError(w, r, err)
		return
	}
	h.renderProfile(w, r, token)
}

func (h *handlers) revokeUserToken(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := h.store.RevokeUserToken(r.Context(), user.ID, id); err != nil {
		h.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/profile", http.StatusSeeOther)
}

func (h *handlers) renderProfile(w http.ResponseWriter, r *http.Request, newToken string) {
	user := currentUser(r)
	scores, err := h.submissions.UserCourseScores(r.Context(), user.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	stats, err := h.submissions.UserStats(r.Context(), user.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	tokens, err := h.store.ListUserTokens(r.Context(), user.ID)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, views.Profile(user, scores, stats, tokens, newToken))
}
