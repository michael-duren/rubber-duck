package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mduren/getcracked/internal/auth"
	"github.com/mduren/getcracked/internal/domain"
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

func TestSubmitBySlugAndPollStatus(t *testing.T) {
	mux, fs := testMux(t)
	fs.users["alice"] = fakeUser{id: 1, hash: "x"}
	fs.variant = &domain.Variant{
		Language: "go",
		Lessons: []domain.Lesson{
			{Slug: "l1", Challenges: []domain.Challenge{{ID: 42, Slug: "concurrent-sum"}}},
		},
		Final: domain.Challenge{ID: 99, Slug: "final"},
	}
	fs.variantSlug = "intro-to-concurrency"

	token, hash := auth.NewUserToken()
	fs.CreateUserToken(context.Background(), 1, "cli", hash)

	rec := bearerPost(mux, "/courses/intro-to-concurrency/go/challenges/concurrent-sum/submissions", token, url.Values{"code": {"package challenge"}})
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit by slug = %d body %s", rec.Code, rec.Body)
	}
	var created struct {
		ID  int64  `json:"id"`
		URL string `json:"url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if created.ID != fs.submissions[created.ID].ID || fs.submissions[created.ID].ChallengeID != 42 {
		t.Fatalf("submission not recorded against challenge 42: %+v", fs.submissions)
	}

	req := httptest.NewRequest("GET", created.URL+"/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	statusRec := httptest.NewRecorder()
	mux.ServeHTTP(statusRec, req)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("status poll = %d body %s", statusRec.Code, statusRec.Body)
	}
	var status struct {
		Status string `json:"status"`
		Score  int    `json:"score"`
	}
	json.Unmarshal(statusRec.Body.Bytes(), &status)
	if status.Status != "passed" || status.Score != 10 {
		t.Errorf("status = %+v, want passed/10", status)
	}

	// Unknown challenge slug is 404, not a panic or a submission against
	// the wrong challenge.
	rec = bearerPost(mux, "/courses/intro-to-concurrency/go/challenges/nope/submissions", token, url.Values{"code": {"x"}})
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown challenge slug = %d, want 404", rec.Code)
	}
}

// A submission carrying a CLI-claimed verdict is stored already graded —
// scored proportionally from the claimed output — and returned in the POST
// response so the CLI never polls.
func TestSubmitBySlugWithClaimedVerdict(t *testing.T) {
	mux, fs := testMux(t)
	fs.users["alice"] = fakeUser{id: 1, hash: "x"}
	fs.variant = &domain.Variant{
		Language: "go",
		Lessons: []domain.Lesson{
			{Slug: "l1", Challenges: []domain.Challenge{{ID: 42, Slug: "concurrent-sum", Points: 10}}},
		},
	}
	fs.variantSlug = "intro-to-concurrency"

	token, hash := auth.NewUserToken()
	fs.CreateUserToken(context.Background(), 1, "cli", hash)

	// 3 of 4 leaf tests passed locally: proportional credit, round(10*3/4)=8.
	out := "--- PASS: TestA\n--- PASS: TestB\n--- PASS: TestC\n--- FAIL: TestD\n"
	rec := bearerPost(mux, "/courses/intro-to-concurrency/go/challenges/concurrent-sum/submissions", token, url.Values{
		"code":           {"package challenge"},
		"claimed_status": {"failed"},
		"claimed_output": {out},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("claimed submit = %d body %s", rec.Code, rec.Body)
	}
	var created struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
		Score  int    `json:"score"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if created.Status != "failed" || created.Score != 8 {
		t.Errorf("claimed response = %+v, want failed/8", created)
	}
	sub := fs.submissions[created.ID]
	if !sub.Claimed || sub.Status != "failed" || sub.Score != 8 {
		t.Errorf("stored submission = %+v, want claimed failed/8", sub)
	}

	// Only passed/failed are claimable; anything else is a 400, not stored.
	rec = bearerPost(mux, "/courses/intro-to-concurrency/go/challenges/concurrent-sum/submissions", token, url.Values{
		"code":           {"package challenge"},
		"claimed_status": {"error"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("claimed_status=error = %d, want 400", rec.Code)
	}
}
