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

	"github.com/mduren/getcracked/internal/domain"
)

// fakeStore is an in-memory AuthStore.
type fakeStore struct {
	users    map[string]fakeUser // by username
	sessions map[string]int64    // session token hash -> user id
	tokens   map[int64]fakeToken // CLI token id -> token
	nextTok  int64
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
		tokens: map[int64]fakeToken{},
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

// CourseReader stubs: auth tests don't exercise course pages beyond the
// (empty) catalog rendered at "/".
func (f *fakeStore) ListCourses(context.Context) ([]domain.CourseSummary, error) { return nil, nil }

func (f *fakeStore) CourseBySlug(context.Context, string) (domain.Course, []domain.VariantSummary, error) {
	return domain.Course{}, nil, domain.ErrNotFound
}

func (f *fakeStore) VariantDetail(context.Context, string, string) (domain.Course, domain.Variant, error) {
	return domain.Course{}, domain.Variant{}, domain.ErrNotFound
}

// SubmissionStore stubs.
func (f *fakeStore) CreateSubmission(context.Context, int64, int64, string) (int64, error) {
	return 1, nil
}

func (f *fakeStore) SubmissionForUser(context.Context, int64, int64) (domain.Submission, error) {
	return domain.Submission{}, domain.ErrNotFound
}

func (f *fakeStore) UserCourseScores(context.Context, int64) ([]domain.CourseScore, error) {
	return nil, nil
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

func postForm(mux *http.ServeMux, path string, form url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
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
