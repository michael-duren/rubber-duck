// Package httpapi is the JSON API under /api/v1: public course reads (the
// duck CLI's pull/test flows and the repo-mirror export) plus the
// user-token-authenticated proposal endpoints behind `duck propose`.
package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/michael-duren/rubber-duck/internal/auth"
	"github.com/michael-duren/rubber-duck/internal/domain"
)

// UserStore resolves human user tokens (the only API credential; agent API
// keys are gone with the agent publish endpoints).
type UserStore interface {
	// UserByToken resolves an unrevoked "gc_u_" user token hash to its
	// owning user. Implementations return domain.ErrNotFound for an
	// unknown or revoked token.
	UserByToken(ctx context.Context, tokenHash []byte) (domain.User, error)
}

// ctxKey is this package's own request-context key for the authenticated
// user. It's intentionally independent from internal/web's
// identically-purposed but unexported ctxKey type in session.go: the two
// packages don't share an auth-context abstraction (see this codebase's
// pattern of small per-package auth helpers).
type ctxKey struct{}

// currentUser returns the authenticated user for this request. Never nil
// behind requireUser.
func currentUser(r *http.Request) *domain.User {
	u, _ := r.Context().Value(ctxKey{}).(*domain.User)
	return u
}

// requireUser rejects requests without a valid "Authorization: Bearer
// gc_u_…" user token and attaches the resolved domain.User to the request
// context (see currentUser). Anything that isn't a user token — including
// an old "gc_" agent API key — is a plain 401: the agent publish API is
// gone, and course changes go through proposals.
func requireUser(logger *slog.Logger, us UserStore, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token", nil)
			return
		}
		if !strings.HasPrefix(token, auth.UserTokenPrefix) {
			writeError(w, http.StatusUnauthorized, "unauthorized",
				"expected a user token (mint one on /profile or via `duck auth login`)", nil)
			return
		}
		user, err := us.UserByToken(r.Context(), auth.HashToken(token))
		switch {
		case errors.Is(err, domain.ErrNotFound):
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or revoked user token", nil)
			return
		case err != nil:
			logger.Error("auth check failed", "path", r.URL.Path, "err", err)
			writeError(w, http.StatusInternalServerError, "internal", "auth check failed", nil)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, &user))
		next.ServeHTTP(w, r)
	})
}
