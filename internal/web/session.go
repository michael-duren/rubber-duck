package web

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/michael-duren/rubber-duck/internal/auth"
	"github.com/michael-duren/rubber-duck/internal/domain"
)

const (
	sessionCookie = "gc_session"
	sessionTTL    = 30 * 24 * time.Hour
)

// AuthStore is the slice of the store the web layer needs for accounts.
type AuthStore interface {
	CreateUser(ctx context.Context, username, passwordHash string) (domain.User, error)
	UserByUsername(ctx context.Context, username string) (domain.User, string, error)
	CreateSession(ctx context.Context, tokenHash []byte, userID int64, expiresAt time.Time) error
	UserBySession(ctx context.Context, tokenHash []byte) (domain.User, error)
	DeleteSession(ctx context.Context, tokenHash []byte) error
	CreateUserToken(ctx context.Context, userID int64, name string, tokenHash []byte) (int64, error)
	UserByToken(ctx context.Context, tokenHash []byte) (domain.User, error)
	ListUserTokens(ctx context.Context, userID int64) ([]domain.UserToken, error)
	RevokeUserToken(ctx context.Context, userID, tokenID int64) error
	PasswordHash(ctx context.Context, userID int64) (string, error)
	UpdatePassword(ctx context.Context, userID int64, passwordHash string) error
	DeleteOtherSessions(ctx context.Context, userID int64, keepTokenHash []byte) error
}

type ctxKey struct{}

// currentUser returns the logged-in user, or nil.
func currentUser(r *http.Request) *domain.User {
	u, _ := r.Context().Value(ctxKey{}).(*domain.User)
	return u
}

// bearerToken extracts a CLI token from an Authorization: Bearer header.
func bearerToken(r *http.Request) (string, bool) {
	return strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
}

// withUser resolves the caller's identity — a CLI bearer token takes
// priority over the session cookie — and attaches the user to the request
// context. It never rejects: pages decide themselves what anonymous
// visitors may see.
func (h *handlers) withUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token, ok := bearerToken(r); ok {
			if u, err := h.store.UserByToken(r.Context(), auth.HashToken(token)); err == nil {
				r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, &u))
			}
		} else if c, err := r.Cookie(sessionCookie); err == nil {
			if u, err := h.store.UserBySession(r.Context(), auth.HashToken(c.Value)); err == nil {
				r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, &u))
			}
		}
		next.ServeHTTP(w, r)
	})
}

// requireUser rejects anonymous requests: 401 for a bearer-token caller
// (a CLI, not a browser to redirect), otherwise a redirect to /login.
func (h *handlers) requireUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if currentUser(r) == nil {
			if _, ok := bearerToken(r); ok {
				http.Error(w, "invalid or revoked token", http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

func (h *handlers) setSession(w http.ResponseWriter, r *http.Request, userID int64) error {
	token, hash := auth.NewSessionToken()
	if err := h.store.CreateSession(r.Context(), hash, userID, time.Now().Add(sessionTTL)); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		// Behind Cloud Run's proxy TLS terminates upstream, so r.TLS is nil.
		Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
	})
	return nil
}
