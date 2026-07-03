package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mduren/getcracked/internal/auth"
)

func bearerPost(mux *http.ServeMux, path, token string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestUserTokenAuthenticatesSubmission(t *testing.T) {
	mux, fs := testMux(t)
	fs.users["alice"] = fakeUser{id: 1, hash: "x"}

	token, hash := auth.NewUserToken()
	if _, err := fs.CreateUserToken(context.Background(), 1, "laptop", hash); err != nil {
		t.Fatalf("create token: %v", err)
	}

	rec := bearerPost(mux, "/challenges/1/submissions", token, url.Values{"code": {"package x"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("submission with valid token = %d, want 303: %s", rec.Code, rec.Body)
	}

	// A garbage token is rejected, not silently treated as anonymous.
	rec = bearerPost(mux, "/challenges/1/submissions", "gc_u_wrong", url.Values{"code": {"package x"}})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("submission with bad token = %d, want 401", rec.Code)
	}

	// Revoking the token turns a previously valid bearer into 401.
	for id, tk := range fs.tokens {
		if tk.hash == string(hash) {
			if err := fs.RevokeUserToken(context.Background(), 1, id); err != nil {
				t.Fatalf("revoke: %v", err)
			}
		}
	}
	rec = bearerPost(mux, "/challenges/1/submissions", token, url.Values{"code": {"package x"}})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("submission with revoked token = %d, want 401", rec.Code)
	}
}
