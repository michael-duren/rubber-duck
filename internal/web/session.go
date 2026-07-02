package web

import (
	"context"
	"net/http"
	"time"

	"github.com/mduren/getcracked/internal/auth"
	"github.com/mduren/getcracked/internal/domain"
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
}

type ctxKey struct{}

// currentUser returns the logged-in user, or nil.
func currentUser(r *http.Request) *domain.User {
	u, _ := r.Context().Value(ctxKey{}).(*domain.User)
	return u
}

// withUser resolves the session cookie (if any) and attaches the user to the
// request context. It never rejects: pages decide themselves what anonymous
// visitors may see.
func (h *handlers) withUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(sessionCookie); err == nil {
			u, err := h.store.UserBySession(r.Context(), auth.HashToken(c.Value))
			if err == nil {
				r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, &u))
			}
		}
		next.ServeHTTP(w, r)
	})
}

// requireUser redirects anonymous visitors to /login.
func (h *handlers) requireUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if currentUser(r) == nil {
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
		Secure:   r.TLS != nil,
	})
	return nil
}
