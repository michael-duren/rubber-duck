// Package httpapi is the agent-facing JSON API under /api/v1.
package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/mduren/getcracked/internal/auth"
)

// KeyStore validates agent API keys.
type KeyStore interface {
	APIKeyValid(ctx context.Context, keyHash []byte) (bool, error)
}

// requireKey rejects requests without a valid "Authorization: Bearer gc_…" key.
func requireKey(ks KeyStore, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token", nil)
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
