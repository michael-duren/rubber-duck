package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestCSRFRejectsPostWithoutToken(t *testing.T) {
	mux, _ := testMux(t)

	// No csrf cookie, no csrf_token field at all.
	req := httptest.NewRequest("POST", "/signup", strings.NewReader(url.Values{
		"username": {"bob"}, "password": {"supersecret"},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("no token at all: status = %d, want 403", rec.Code)
	}

	// Cookie present but form field missing/wrong is still rejected.
	csrf := fetchCSRFCookie(mux)
	if csrf == nil {
		t.Fatal("expected a csrf cookie on GET /")
	}
	req = httptest.NewRequest("POST", "/signup", strings.NewReader(url.Values{
		"username": {"bob"}, "password": {"supersecret"}, "csrf_token": {"wrong-value"},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrf)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("mismatched token: status = %d, want 403", rec.Code)
	}

	// Matching cookie + form field succeeds.
	rec = postForm(mux, "/signup", url.Values{"username": {"bob"}, "password": {"supersecret"}}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("valid token: status = %d, want 303", rec.Code)
	}

	// A GET is never CSRF-checked (no state change).
	req = httptest.NewRequest("GET", "/", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET / = %d, want 200", rec.Code)
	}

	// Bearer-authenticated requests are exempt (no cookies involved).
	rec = bearerPost(mux, "/challenges/1/submissions", "gc_u_whatever", url.Values{"code": {"x"}})
	if rec.Code == http.StatusForbidden {
		t.Errorf("bearer request got 403 from CSRF check, want exemption")
	}
}

// TestFormBodyLimit covers withCSRF's MaxBytesReader cap: a form body over
// maxFormBytes is refused with 413 before any handler buffers it.
func TestFormBodyLimit(t *testing.T) {
	mux, _ := testMux(t)
	csrf := fetchCSRFCookie(mux)
	body := "csrf_token=" + csrf.Value + "&markdown=" + strings.Repeat("a", maxFormBytes+1)
	req := httptest.NewRequest("POST", "/signup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrf)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized form body: status = %d, want 413", rec.Code)
	}
}
