package web

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/mduren/getcracked/internal/auth"
)

func TestSubmissionRateLimitReturns429(t *testing.T) {
	mux, fs := testMux(t)
	fs.users["alice"] = fakeUser{id: 1, hash: "x"}
	fs.rateLimit = func(userID, challengeID int64) bool { return true }

	token, hash := auth.NewUserToken()
	fs.CreateUserToken(t.Context(), 1, "cli", hash)

	rec := bearerPost(mux, "/challenges/1/submissions", token, url.Values{"code": {"package x"}})
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("rate-limited submit = %d, want 429: %s", rec.Code, rec.Body)
	}
}
