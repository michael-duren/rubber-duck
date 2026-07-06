// Package httpapi is the agent-facing JSON API under /api/v1.
package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/michael-duren/rubber-duck/internal/auth"
	"github.com/michael-duren/rubber-duck/internal/domain"
)

// userTokenPrefix identifies a human CLI token (internal/auth.NewUserToken)
// rather than an agent API key (internal/auth.NewAPIKey, prefix "gc_"). It
// must be checked before the plain "gc_" prefix since it's a longer, more
// specific prefix of it.
const userTokenPrefix = "gc_u_"

// KeyStore validates agent API keys and resolves human user tokens.
type KeyStore interface {
	APIKeyValid(ctx context.Context, keyHash []byte) (bool, error)
	// UserByToken resolves an unrevoked "gc_u_" user token hash to its
	// owning user. Implementations return domain.ErrNotFound for an
	// unknown or revoked token.
	UserByToken(ctx context.Context, tokenHash []byte) (domain.User, error)
}

// ctxKey is this package's own request-context key for the authenticated
// human user, if any. It's intentionally independent from internal/web's
// identically-purposed but unexported ctxKey type in session.go: the two
// packages don't share an auth-context abstraction (see this codebase's
// pattern of small per-package auth helpers).
type ctxKey struct{}

// currentUser returns the authenticated human user for this request, or nil
// if the caller authenticated with a plain agent API key (or the route
// doesn't require auth at all).
func currentUser(r *http.Request) *domain.User {
	u, _ := r.Context().Value(ctxKey{}).(*domain.User)
	return u
}

// requireKey rejects requests without a valid "Authorization: Bearer …"
// credential. Two forms are accepted: a "gc_u_"-prefixed human user token,
// which attaches the resolved domain.User to the request context (see
// currentUser) so writes can be attributed and versioned; and a plain
// "gc_"-prefixed agent API key, which behaves exactly as before (no user
// attached, writes stay unattributed and unversioned).
func requireKey(ks KeyStore, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token", nil)
			return
		}

		if strings.HasPrefix(token, userTokenPrefix) {
			user, err := ks.UserByToken(r.Context(), auth.HashToken(token))
			switch {
			case errors.Is(err, domain.ErrNotFound):
				writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or revoked user token", nil)
				return
			case err != nil:
				writeError(w, http.StatusInternalServerError, "internal", "auth check failed", nil)
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, &user))
			next.ServeHTTP(w, r)
			return
		}

		valid, err := ks.APIKeyValid(r.Context(), auth.HashToken(token))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "auth check failed", nil)
			return
		}
		if !valid {
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or revoked api key", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}
