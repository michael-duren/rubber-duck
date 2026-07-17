package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"

	"github.com/michael-duren/rubber-duck/internal/web/views"
)

// maxFormBytes caps form POST bodies before anything parses them into
// memory. The largest legitimate form is the markdown editor's save/preview
// carrying a full course document (the agent API caps those at 2 MiB);
// URL-encoding can roughly triple that, so 8 MiB leaves ample headroom while
// still bounding what one request can make the server buffer.
const maxFormBytes = 8 << 20

// csrfCookie's name is mirrored in cmd/duck's login tests (fakeCSRFCookie),
// whose fake server enforces this middleware's double-submit contract.
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
		// Bound every state-changing body (bearer or browser) up front, so no
		// downstream FormValue can buffer an unbounded request into memory.
		if isUnsafeMethod(r.Method) {
			r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
		}

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

		if isUnsafeMethod(r.Method) {
			// Parse explicitly so an over-limit body gets a 413, not a
			// misleading "invalid csrf token" from FormValue's swallowed error.
			if err := r.ParseForm(); err != nil {
				status := http.StatusBadRequest
				var mbe *http.MaxBytesError
				if errors.As(err, &mbe) {
					status = http.StatusRequestEntityTooLarge
				}
				http.Error(w, "invalid form body", status)
				return
			}
			if subtle.ConstantTimeCompare([]byte(r.FormValue("csrf_token")), []byte(token)) != 1 {
				http.Error(w, "invalid or missing csrf token", http.StatusForbidden)
				return
			}
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
