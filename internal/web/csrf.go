package web

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/mduren/getcracked/internal/web/views"
)

const csrfCookie = "gc_csrf"

func newCSRFToken() string {
	b := make([]byte, 20)
	rand.Read(b) // never fails; see crypto/rand docs
	return hex.EncodeToString(b)
}

// withCSRF is a double-submit-cookie check: a hidden form field must match
// an httponly cookie only the server can set, so a cross-site form (which
// can't read or set that cookie) can't forge a matching value. It issues
// the cookie on first contact and attaches its value to the context so
// templates can render it into forms via views.WithCSRFToken.
//
// Bearer-token requests (the gc CLI, /api/v1) carry no cookies at all —
// they're not vulnerable to CSRF in the first place, since a browser never
// attaches an Authorization header to a cross-site form submission — so
// they're exempt from both the cookie and the check.
func (h *handlers) withCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := bearerToken(r); ok {
			next.ServeHTTP(w, r)
			return
		}

		token := ""
		if c, err := r.Cookie(csrfCookie); err == nil {
			token = c.Value
		}
		if token == "" {
			token = newCSRFToken()
			http.SetCookie(w, &http.Cookie{
				Name:     csrfCookie,
				Value:    token,
				Path:     "/",
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
				Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
			})
		}
		r = r.WithContext(views.WithCSRFToken(r.Context(), token))

		if isUnsafeMethod(r.Method) && r.FormValue("csrf_token") != token {
			http.Error(w, "invalid or missing csrf token", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isUnsafeMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}
