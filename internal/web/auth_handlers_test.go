package web

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/michael-duren/rubber-duck/internal/domain"
)

// fakeStore is an in-memory AuthStore.
type fakeStore struct {
	users       map[string]fakeUser // by username
	sessions    map[string]int64    // session token hash -> user id
	tokens      map[int64]fakeToken // CLI token id -> token
	nextTok     int64
	variant     *domain.Variant // set by tests that need VariantDetail to resolve
	variantSlug string
	submissions map[int64]domain.Submission
	nextSub     int64
	rateLimit   func(userID, challengeID int64) bool // nil = never limited
}

type fakeUser struct {
	id   int64
	hash string
}

type fakeToken struct {
	userID  int64
	name    string
	hash    string
	revoked bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		users: map[string]fakeUser{}, sessions: map[string]int64{},
		tokens: map[int64]fakeToken{}, submissions: map[int64]domain.Submission{},
	}
}

func (f *fakeStore) CreateUser(_ context.Context, username, passwordHash string) (domain.User, error) {
	if _, ok := f.users[username]; ok {
		return domain.User{}, domain.ErrUsernameTaken
	}
	id := int64(len(f.users) + 1)
	f.users[username] = fakeUser{id: id, hash: passwordHash}
	return domain.User{ID: id, Username: username}, nil
}

func (f *fakeStore) UserByUsername(_ context.Context, username string) (domain.User, string, error) {
	u, ok := f.users[username]
	if !ok {
		return domain.User{}, "", domain.ErrNotFound
	}
	return domain.User{ID: u.id, Username: username}, u.hash, nil
}

func (f *fakeStore) CreateSession(_ context.Context, tokenHash []byte, userID int64, _ time.Time) error {
	f.sessions[string(tokenHash)] = userID
	return nil
}

func (f *fakeStore) UserBySession(_ context.Context, tokenHash []byte) (domain.User, error) {
	id, ok := f.sessions[string(tokenHash)]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	for name, u := range f.users {
		if u.id == id {
			return domain.User{ID: id, Username: name}, nil
		}
	}
	return domain.User{}, domain.ErrNotFound
}

func (f *fakeStore) DeleteSession(_ context.Context, tokenHash []byte) error {
	delete(f.sessions, string(tokenHash))
	return nil
}

func (f *fakeStore) CreateUserToken(_ context.Context, userID int64, name string, tokenHash []byte) (int64, error) {
	f.nextTok++
	f.tokens[f.nextTok] = fakeToken{userID: userID, name: name, hash: string(tokenHash)}
	return f.nextTok, nil
}

func (f *fakeStore) UserByToken(_ context.Context, tokenHash []byte) (domain.User, error) {
	for _, t := range f.tokens {
		if t.hash == string(tokenHash) && !t.revoked {
			for name, u := range f.users {
				if u.id == t.userID {
					return domain.User{ID: u.id, Username: name}, nil
				}
			}
			return domain.User{ID: t.userID}, nil
		}
	}
	return domain.User{}, domain.ErrNotFound
}

func (f *fakeStore) ListUserTokens(_ context.Context, userID int64) ([]domain.UserToken, error) {
	var out []domain.UserToken
	for id, t := range f.tokens {
		if t.userID == userID {
			out = append(out, domain.UserToken{ID: id, Name: t.name})
		}
	}
	return out, nil
}

func (f *fakeStore) RevokeUserToken(_ context.Context, userID, tokenID int64) error {
	t, ok := f.tokens[tokenID]
	if !ok || t.userID != userID || t.revoked {
		return domain.ErrNotFound
	}
	t.revoked = true
	f.tokens[tokenID] = t
	return nil
}

func (f *fakeStore) PasswordHash(_ context.Context, userID int64) (string, error) {
	for _, u := range f.users {
		if u.id == userID {
			return u.hash, nil
		}
	}
	return "", domain.ErrNotFound
}

func (f *fakeStore) UpdatePassword(_ context.Context, userID int64, passwordHash string) error {
	for name, u := range f.users {
		if u.id == userID {
			u.hash = passwordHash
			f.users[name] = u
			return nil
		}
	}
	return domain.ErrNotFound
}

func (f *fakeStore) DeleteOtherSessions(_ context.Context, userID int64, keepTokenHash []byte) error {
	for hash, uid := range f.sessions {
		if uid == userID && hash != string(keepTokenHash) {
			delete(f.sessions, hash)
		}
	}
	return nil
}

// CourseReader stubs: auth tests don't exercise course pages beyond the
// (empty) catalog rendered at "/".
func (f *fakeStore) ListCourses(context.Context) ([]domain.CourseSummary, error) { return nil, nil }

func (f *fakeStore) CourseBySlug(context.Context, string) (domain.Course, []domain.VariantSummary, error) {
	return domain.Course{}, nil, domain.ErrNotFound
}

func (f *fakeStore) VariantDetail(_ context.Context, slug, lang string) (domain.Course, domain.Variant, error) {
	if f.variant == nil || slug != f.variantSlug || lang != f.variant.Language {
		return domain.Course{}, domain.Variant{}, domain.ErrNotFound
	}
	return domain.Course{Slug: slug}, *f.variant, nil
}

// SubmissionStore stubs.
func (f *fakeStore) CreateSubmission(_ context.Context, userID, challengeID int64, code string) (int64, error) {
	f.nextSub++
	f.submissions[f.nextSub] = domain.Submission{ID: f.nextSub, UserID: userID, ChallengeID: challengeID, Code: code, Status: "passed", Score: 10}
	return f.nextSub, nil
}

func (f *fakeStore) CreateClaimedSubmission(_ context.Context, userID, challengeID int64, code, status, output string, score int, testsPassed, testsTotal *int) (int64, error) {
	f.nextSub++
	f.submissions[f.nextSub] = domain.Submission{
		ID: f.nextSub, UserID: userID, ChallengeID: challengeID, Code: code,
		Status: status, Output: output, Score: score,
		TestsPassed: testsPassed, TestsTotal: testsTotal, Claimed: true,
	}
	return f.nextSub, nil
}

func (f *fakeStore) SubmissionForUser(_ context.Context, id, userID int64) (domain.Submission, error) {
	sub, ok := f.submissions[id]
	if !ok || sub.UserID != userID {
		return domain.Submission{}, domain.ErrNotFound
	}
	return sub, nil
}

func (f *fakeStore) UserCourseScores(context.Context, int64) ([]domain.CourseScore, error) {
	return nil, nil
}

func (f *fakeStore) SubmissionRateLimited(_ context.Context, userID, challengeID int64) (bool, error) {
	if f.rateLimit == nil {
		return false, nil
	}
	return f.rateLimit(userID, challengeID), nil
}

func (f *fakeStore) CompletedChallenges(_ context.Context, userID, variantID int64) (map[int64]bool, error) {
	completed := map[int64]bool{}
	for _, sub := range f.submissions {
		if sub.UserID == userID && sub.Status == "passed" {
			completed[sub.ChallengeID] = true
		}
	}
	return completed, nil
}

func (f *fakeStore) LatestSubmissionCodesByVariant(_ context.Context, userID, variantID int64) (map[int64]string, error) {
	result := make(map[int64]string)
	for _, sub := range f.submissions {
		if sub.UserID == userID && sub.Code != "" {
			if _, exists := result[sub.ChallengeID]; !exists {
				result[sub.ChallengeID] = sub.Code
			}
		}
	}
	return result, nil
}

func (f *fakeStore) SubmissionsForChallenge(_ context.Context, userID, challengeID int64) ([]domain.Submission, error) {
	var result []domain.Submission
	for _, sub := range f.submissions {
		if sub.UserID == userID && sub.ChallengeID == challengeID {
			result = append(result, sub)
		}
	}
	return result, nil
}

type noopEnqueuer struct{}

func (noopEnqueuer) Enqueue(int64) {}

func testMux(t *testing.T) (*http.ServeMux, *fakeStore) {
	t.Helper()
	mux := http.NewServeMux()
	fs := newFakeStore()
	Register(mux, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), fs, fs, fs, noopEnqueuer{})
	return mux, fs
}

// fetchCSRFCookie makes a throwaway GET to obtain a fresh double-submit
// CSRF cookie; its value is valid for any single subsequent request that
// presents both the cookie and a matching csrf_token form field.
func fetchCSRFCookie(mux *http.ServeMux) *http.Cookie {
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.Name == csrfCookie {
			return c
		}
	}
	return nil
}

func postForm(mux *http.ServeMux, path string, form url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	csrf := fetchCSRFCookie(mux)
	if csrf != nil {
		form = cloneValues(form)
		form.Set("csrf_token", csrf.Value)
	}
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if csrf != nil {
		req.AddCookie(csrf)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func cloneValues(v url.Values) url.Values {
	out := make(url.Values, len(v)+1)
	for k, vs := range v {
		out[k] = append([]string(nil), vs...)
	}
	return out
}

func TestSignup(t *testing.T) {
	cases := []struct {
		name       string
		username   string
		password   string
		preexists  bool
		wantStatus int
		wantBody   string
	}{
		{"ok", "alice", "supersecret", false, http.StatusSeeOther, ""},
		{"short username", "ab", "supersecret", false, http.StatusOK, "3-32 characters"},
		{"short password", "alice", "short", false, http.StatusOK, "at least 8"},
		{"taken", "alice", "supersecret", true, http.StatusOK, "taken"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mux, fs := testMux(t)
			if c.preexists {
				fs.users[c.username] = fakeUser{id: 99, hash: "x"}
			}
			rec := postForm(mux, "/signup", url.Values{"username": {c.username}, "password": {c.password}}, nil)
			if rec.Code != c.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, c.wantStatus)
			}
			if c.wantBody != "" && !strings.Contains(rec.Body.String(), c.wantBody) {
				t.Errorf("body missing %q", c.wantBody)
			}
			if c.wantStatus == http.StatusSeeOther && len(fs.sessions) != 1 {
				t.Errorf("sessions = %d, want 1", len(fs.sessions))
			}
		})
	}
}

func TestLoginLogoutRoundTrip(t *testing.T) {
	mux, fs := testMux(t)

	// Sign up (also logs in) and grab the session cookie.
	rec := postForm(mux, "/signup", url.Values{"username": {"alice"}, "password": {"supersecret"}}, nil)
	res := rec.Result()
	var session *http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == sessionCookie {
			session = c
		}
	}
	if session == nil {
		t.Fatal("no session cookie set on signup")
	}

	// Home page shows the username when logged in.
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(session)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "alice") {
		t.Error("home page does not show logged-in user")
	}

	// Wrong password fails.
	rec = postForm(mux, "/login", url.Values{"username": {"alice"}, "password": {"wrongwrong"}}, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Wrong username or password") {
		t.Errorf("bad login: status %d", rec.Code)
	}

	// Logout clears the session server-side.
	postForm(mux, "/logout", url.Values{}, session)
	if len(fs.sessions) != 0 {
		t.Errorf("sessions after logout = %d, want 0", len(fs.sessions))
	}
}
