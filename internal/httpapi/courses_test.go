package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/michael-duren/rubber-duck/internal/auth"
	"github.com/michael-duren/rubber-duck/internal/domain"
)

type fakeStore struct {
	versions map[string]int // "slug/lang" -> version
	courses  map[string]domain.Course
	sources  map[string]string
	variants map[string]domain.Variant
	editedBy map[string]*int64 // "slug/lang" -> last UpsertVariant's editedBy
	paths    map[string]domain.LearningPath

	users map[string]domain.User // token -> user, for UserByToken

	userByTokenErr error // if set, UserByToken fails with it (models the DB being down)
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		versions: map[string]int{}, courses: map[string]domain.Course{},
		sources: map[string]string{}, variants: map[string]domain.Variant{},
		editedBy: map[string]*int64{}, users: map[string]domain.User{},
		paths: map[string]domain.LearningPath{},
	}
}

// UserByToken looks up a user by the raw token string (tests key f.users by
// the plaintext token rather than a hash, since the fake never sees
// internal/auth.HashToken's output — requireKey hashes before calling this,
// so the fake stores by that same hash to match).
func (f *fakeStore) UserByToken(_ context.Context, tokenHash []byte) (domain.User, error) {
	if f.userByTokenErr != nil {
		return domain.User{}, f.userByTokenErr
	}
	u, ok := f.users[string(tokenHash)]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return u, nil
}

func (f *fakeStore) key(slug, lang string) string { return slug + "/" + lang }

func (f *fakeStore) VariantDetail(_ context.Context, slug, lang string) (domain.Course, domain.Variant, error) {
	c, ok := f.courses[slug]
	if !ok {
		return domain.Course{}, domain.Variant{}, domain.ErrNotFound
	}
	v, ok := f.variants[f.key(slug, lang)]
	if !ok {
		return domain.Course{}, domain.Variant{}, domain.ErrNotFound
	}
	return c, v, nil
}

// UpsertVariant mirrors the real store's optimistic-concurrency contract
// (see internal/store.Store.UpsertVariant and internal/web's identically
// patterned fake in auth_handlers_test.go): a non-nil expectedVersion that
// doesn't match the stored version is rejected with domain.ErrVersionConflict
// and leaves all state untouched.
func (f *fakeStore) UpsertVariant(_ context.Context, c domain.Course, v domain.Variant, editedBy *int64, expectedVersion *int) (int, error) {
	k := f.key(c.Slug, v.Language)
	if expectedVersion != nil && *expectedVersion != f.versions[k] {
		return 0, domain.ErrVersionConflict
	}
	f.variants[k] = v
	f.versions[k]++
	f.courses[c.Slug] = c
	f.sources[k] = v.SourceMD
	f.editedBy[k] = editedBy
	return f.versions[k], nil
}

func (f *fakeStore) ListCourses(context.Context) ([]domain.CourseSummary, error) {
	var out []domain.CourseSummary
	for slug, c := range f.courses {
		out = append(out, domain.CourseSummary{Slug: slug, Title: c.Title, Tags: c.Tags})
	}
	return out, nil
}

func (f *fakeStore) CourseBySlug(_ context.Context, slug string) (domain.Course, []domain.VariantSummary, error) {
	c, ok := f.courses[slug]
	if !ok {
		return domain.Course{}, nil, domain.ErrNotFound
	}
	return c, nil, nil
}

func (f *fakeStore) VariantSource(_ context.Context, slug, lang string) (string, int, error) {
	src, ok := f.sources[f.key(slug, lang)]
	if !ok {
		return "", 0, domain.ErrNotFound
	}
	return src, f.versions[f.key(slug, lang)], nil
}

func (f *fakeStore) DeleteCourse(_ context.Context, slug string) error {
	if _, ok := f.courses[slug]; !ok {
		return domain.ErrNotFound
	}
	delete(f.courses, slug)
	return nil
}

func (f *fakeStore) DeleteVariant(_ context.Context, slug, lang string) error {
	if _, ok := f.sources[f.key(slug, lang)]; !ok {
		return domain.ErrNotFound
	}
	delete(f.sources, f.key(slug, lang))
	return nil
}

func (f *fakeStore) ListTags(context.Context) ([]string, error) { return []string{"backend"}, nil }

// APIKeyValid accepts any bearer token that reaches this method — requireKey
// only calls it for non-"gc_u_" tokens, so this models "any agent key is
// valid" the same way the old standalone allowAllKeys fake did.
func (f *fakeStore) APIKeyValid(context.Context, []byte) (bool, error) { return true, nil }

// addUser registers a user token in the fake so a test can authenticate as
// that user via "Bearer "+token.
func (f *fakeStore) addUser(token string, user domain.User) {
	f.users[string(auth.HashToken(token))] = user
}

func testAPI(t *testing.T) (*http.ServeMux, *fakeStore) {
	t.Helper()
	mux := http.NewServeMux()
	fs := newFakeStore()
	Register(mux, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), fs, fs)
	return mux, fs
}

func doJSON(mux *http.ServeMux, method, path string, body any) *httptest.ResponseRecorder {
	return doJSONAs(mux, method, path, "gc_test", body)
}

// doJSONAs is doJSON with a caller-chosen bearer token, so tests can
// authenticate as either an agent key or a "gc_u_" user token.
func doJSONAs(mux *http.ServeMux, method, path, bearer string, body any) *httptest.ResponseRecorder {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Authorization", "Bearer "+bearer)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func seedDoc(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile("../../seed/intro-to-go.md")
	if err != nil {
		t.Fatal(err)
	}
	return string(src)
}

func TestPutVariant(t *testing.T) {
	doc := seedDoc(t)
	put := func(mux *http.ServeMux, path, markdown string) *httptest.ResponseRecorder {
		return doJSON(mux, "PUT", path, map[string]string{"markdown": markdown})
	}

	t.Run("create then update bumps version", func(t *testing.T) {
		mux, _ := testAPI(t)
		rec := put(mux, "/api/v1/courses/intro-to-concurrency/variants/go", doc)
		if rec.Code != http.StatusCreated {
			t.Fatalf("first put = %d body %s", rec.Code, rec.Body)
		}
		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal first put: %v", err)
		}
		if resp["version"].(float64) != 1 || resp["lessons"].(float64) != 2 ||
			resp["challenges"].(float64) != 3 || resp["total_points"].(float64) != 75 {
			t.Errorf("summary = %v", resp)
		}

		rec = put(mux, "/api/v1/courses/intro-to-concurrency/variants/go", doc)
		if rec.Code != http.StatusOK {
			t.Fatalf("second put = %d", rec.Code)
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp["version"].(float64) != 2 {
			t.Errorf("version = %v, want 2", resp["version"])
		}
	})

	t.Run("slug mismatch is 409", func(t *testing.T) {
		mux, _ := testAPI(t)
		rec := put(mux, "/api/v1/courses/other-course/variants/go", doc)
		if rec.Code != http.StatusConflict {
			t.Errorf("status = %d, want 409", rec.Code)
		}
	})

	t.Run("language mismatch is 409", func(t *testing.T) {
		mux, _ := testAPI(t)
		rec := put(mux, "/api/v1/courses/intro-to-concurrency/variants/python", doc)
		if rec.Code != http.StatusConflict {
			t.Errorf("status = %d, want 409", rec.Code)
		}
	})

	t.Run("invalid markdown is 422 with line details", func(t *testing.T) {
		mux, _ := testAPI(t)
		bad := strings.Replace(doc, "### Tests", "### Test", 1) // break one challenge
		rec := put(mux, "/api/v1/courses/intro-to-concurrency/variants/go", bad)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d body %s", rec.Code, rec.Body)
		}
		var resp struct {
			Error struct {
				Code    string
				Details []struct {
					Line    int
					Message string
				}
			}
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Error.Code != "invalid_course_markdown" || len(resp.Error.Details) == 0 {
			t.Fatalf("error = %+v", resp.Error)
		}
		if resp.Error.Details[0].Line == 0 {
			t.Errorf("detail missing line: %+v", resp.Error.Details[0])
		}
	})

	t.Run("bad d2 diagram is 422, not a 500", func(t *testing.T) {
		mux, _ := testAPI(t)
		// Parses fine (ingest doesn't compile diagrams) but the ```d2
		// fence fails to compile when ToDomain renders the lesson HTML.
		doc := "---\ncourse: c\ntitle: T\nlanguage: go\ndescription: d\n---\n\n" +
			"# Lesson: One {#one}\n\n```d2\nx -> \n```\n\n" +
			"## Challenge: A {#a points=5}\n\nPrompt.\n\n" +
			"### Starter\n\n```go\ncode\n```\n\n### Tests\n\n```go\ntests\n```\n\n" +
			"# Final Challenge: F {#fin points=9}\n\n" +
			"### Starter\n\n```go\ns\n```\n\n### Tests\n\n```go\nt\n```\n"
		rec := put(mux, "/api/v1/courses/c/variants/go", doc)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d body %s, want 422", rec.Code, rec.Body)
		}
		var resp struct {
			Error struct{ Code, Message string }
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Error.Code != "invalid_course_markdown" || !strings.Contains(resp.Error.Message, "d2") {
			t.Errorf("error = %+v, want invalid_course_markdown mentioning d2", resp.Error)
		}
	})

	t.Run("non-json body is 400", func(t *testing.T) {
		mux, _ := testAPI(t)
		req := httptest.NewRequest("PUT", "/api/v1/courses/x/variants/go", strings.NewReader("plain text"))
		req.Header.Set("Authorization", "Bearer gc_test")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("empty markdown is 400", func(t *testing.T) {
		mux, _ := testAPI(t)
		rec := put(mux, "/api/v1/courses/x/variants/go", "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("oversized body is 413, not a JSON error", func(t *testing.T) {
		mux, _ := testAPI(t)
		rec := put(mux, "/api/v1/courses/x/variants/go", strings.Repeat("a", maxDocumentBytes))
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want 413", rec.Code)
		}
		var resp struct {
			Error struct{ Code string }
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Error.Code != "document_too_large" {
			t.Errorf("error code = %q, want document_too_large", resp.Error.Code)
		}
	})
}

// TestPutVariantUserToken covers issue #42: a human authenticated via a
// gc_u_ user token (rather than an agent API key) gets attribution
// (editedBy) and optional optimistic concurrency (expected_version) on PUT.
func TestPutVariantUserToken(t *testing.T) {
	doc := seedDoc(t)
	path := "/api/v1/courses/intro-to-concurrency/variants/go"

	putAs := func(mux *http.ServeMux, bearer string, expectedVersion *int) *httptest.ResponseRecorder {
		body := map[string]any{"markdown": doc}
		if expectedVersion != nil {
			body["expected_version"] = *expectedVersion
		}
		return doJSONAs(mux, "PUT", path, bearer, body)
	}

	t.Run("valid user token attributes the write", func(t *testing.T) {
		mux, fs := testAPI(t)
		fs.addUser("gc_u_alice", domain.User{ID: 42, Username: "alice"})

		rec := putAs(mux, "gc_u_alice", nil)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d body %s", rec.Code, rec.Body)
		}
		got := fs.editedBy[fs.key("intro-to-concurrency", "go")]
		if got == nil || *got != 42 {
			t.Errorf("editedBy = %v, want *42", got)
		}
	})

	t.Run("expected_version mismatch is 409 version_conflict", func(t *testing.T) {
		mux, fs := testAPI(t)
		fs.addUser("gc_u_alice", domain.User{ID: 42, Username: "alice"})

		if rec := putAs(mux, "gc_u_alice", nil); rec.Code != http.StatusCreated {
			t.Fatalf("first put = %d body %s", rec.Code, rec.Body)
		}
		wrong := 999 // stored version is now 1; 999 can never match
		rec := putAs(mux, "gc_u_alice", &wrong)
		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d body %s", rec.Code, rec.Body)
		}
		var resp struct {
			Error struct{ Code string }
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Error.Code != "version_conflict" {
			t.Errorf("error code = %q, want version_conflict", resp.Error.Code)
		}
	})

	t.Run("expected_version match succeeds", func(t *testing.T) {
		mux, fs := testAPI(t)
		fs.addUser("gc_u_alice", domain.User{ID: 42, Username: "alice"})

		if rec := putAs(mux, "gc_u_alice", nil); rec.Code != http.StatusCreated {
			t.Fatalf("first put = %d body %s", rec.Code, rec.Body)
		}
		one := 1
		rec := putAs(mux, "gc_u_alice", &one)
		if rec.Code != http.StatusOK {
			t.Fatalf("second put with matching expected_version = %d body %s", rec.Code, rec.Body)
		}
	})

	t.Run("expected_version omitted succeeds like an unversioned write", func(t *testing.T) {
		mux, fs := testAPI(t)
		fs.addUser("gc_u_alice", domain.User{ID: 42, Username: "alice"})

		if rec := putAs(mux, "gc_u_alice", nil); rec.Code != http.StatusCreated {
			t.Fatalf("first put = %d body %s", rec.Code, rec.Body)
		}
		rec := putAs(mux, "gc_u_alice", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("second put without expected_version = %d body %s", rec.Code, rec.Body)
		}
	})

	t.Run("agent key ignores expected_version instead of erroring", func(t *testing.T) {
		mux, _ := testAPI(t)
		if rec := doJSON(mux, "PUT", path, map[string]string{"markdown": doc}); rec.Code != http.StatusCreated {
			t.Fatalf("first put = %d body %s", rec.Code, rec.Body)
		}
		// An agent key sending a wildly wrong expected_version must be a
		// no-op, not a 409: agent publishes stay unversioned.
		wrong := 999
		rec := putAs(mux, "gc_test", &wrong)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body %s, want 200 (expected_version ignored for agent keys)", rec.Code, rec.Body)
		}
	})

	t.Run("unknown user token is 401", func(t *testing.T) {
		mux, _ := testAPI(t)
		rec := putAs(mux, "gc_u_nobody", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	// Every other subtest hand-builds its token, so this is the one place
	// that pins auth.NewUserToken's minted prefix to requireKey's dispatch:
	// if the two drifted, this fails while the hand-built tokens stay green.
	t.Run("token minted by auth.NewUserToken authenticates as its user", func(t *testing.T) {
		mux, fs := testAPI(t)
		token, _ := auth.NewUserToken()
		fs.addUser(token, domain.User{ID: 7, Username: "minty"})

		rec := putAs(mux, token, nil)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d body %s", rec.Code, rec.Body)
		}
		got := fs.editedBy[fs.key("intro-to-concurrency", "go")]
		if got == nil || *got != 7 {
			t.Errorf("editedBy = %v, want *7", got)
		}
	})

	// The pull half of the duck educator round trip: GET authenticated by a
	// user token returns the markdown plus the version to round-trip back.
	t.Run("GET variant source works with a user token", func(t *testing.T) {
		mux, fs := testAPI(t)
		fs.addUser("gc_u_alice", domain.User{ID: 42, Username: "alice"})
		if rec := putAs(mux, "gc_u_alice", nil); rec.Code != http.StatusCreated {
			t.Fatalf("seed put = %d body %s", rec.Code, rec.Body)
		}

		rec := doJSONAs(mux, "GET", path, "gc_u_alice", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body %s", rec.Code, rec.Body)
		}
		var got map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if got["markdown"] != doc || got["version"].(float64) != 1 {
			t.Errorf("got version %v and markdown match %t, want version 1 and matching markdown", got["version"], got["markdown"] == doc)
		}
	})
}

// TestRequireKeyStoreError covers auth-infrastructure failure (as opposed to
// a bad credential): a store error must surface as a logged 500, not a 401,
// and must not fall through to agent-key validation.
func TestRequireKeyStoreError(t *testing.T) {
	var logBuf bytes.Buffer
	mux := http.NewServeMux()
	fs := newFakeStore()
	fs.userByTokenErr = errors.New("db down")
	Register(mux, slog.New(slog.NewTextHandler(&logBuf, nil)), fs, fs)

	rec := doJSONAs(mux, "GET", "/api/v1/courses", "gc_u_whatever", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body %s, want 500", rec.Code, rec.Body)
	}
	var resp struct {
		Error struct{ Code string }
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Error.Code != "internal" {
		t.Errorf("error code = %q, want internal", resp.Error.Code)
	}
	if !strings.Contains(logBuf.String(), "auth check failed") || !strings.Contains(logBuf.String(), "db down") {
		t.Errorf("log output %q should record the auth failure and its cause", logBuf.String())
	}
}

func TestRoundTripAndDelete(t *testing.T) {
	mux, _ := testAPI(t)
	doc := seedDoc(t)
	doJSON(mux, "PUT", "/api/v1/courses/intro-to-concurrency/variants/go", map[string]string{"markdown": doc})

	rec := doJSON(mux, "GET", "/api/v1/courses/intro-to-concurrency/variants/go", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get source = %d", rec.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal get response: %v", err)
	}
	if got["markdown"] != doc {
		t.Error("round-tripped markdown differs from submitted document")
	}
	if got["version"].(float64) != 1 {
		t.Errorf("version = %v, want 1", got["version"])
	}

	if rec := doJSON(mux, "DELETE", "/api/v1/courses/intro-to-concurrency/variants/go", nil); rec.Code != http.StatusNoContent {
		t.Errorf("delete variant = %d", rec.Code)
	}
	if rec := doJSON(mux, "DELETE", "/api/v1/courses/intro-to-concurrency/variants/go", nil); rec.Code != http.StatusNotFound {
		t.Errorf("second delete = %d, want 404", rec.Code)
	}
	if rec := doJSON(mux, "DELETE", "/api/v1/courses/intro-to-concurrency", nil); rec.Code != http.StatusNoContent {
		t.Errorf("delete course = %d", rec.Code)
	}
}

func TestListChallengesPublic(t *testing.T) {
	mux, _ := testAPI(t)
	doc := seedDoc(t)
	doJSON(mux, "PUT", "/api/v1/courses/intro-to-concurrency/variants/go", map[string]string{"markdown": doc})

	req := httptest.NewRequest("GET", "/api/v1/courses/intro-to-concurrency/variants/go/challenges", nil)
	rec := httptest.NewRecorder() // no Authorization header
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body)
	}

	var resp struct {
		Challenges []struct {
			LessonSlug  string `json:"lesson_slug"`
			Slug        string `json:"slug"`
			Title       string `json:"title"`
			Points      int    `json:"points"`
			StarterCode string `json:"starter_code"`
			TestCode    string `json:"test_code"`
		} `json:"challenges"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Challenges) != 3 {
		t.Fatalf("challenges = %d, want 3: %+v", len(resp.Challenges), resp.Challenges)
	}
	for _, c := range resp.Challenges {
		if c.Slug == "" || c.StarterCode == "" || c.TestCode == "" {
			t.Errorf("challenge missing fields: %+v", c)
		}
	}
	last := resp.Challenges[len(resp.Challenges)-1]
	if last.LessonSlug != "" || last.Slug != "final" {
		t.Errorf("final challenge = %+v, want lesson_slug empty and slug final", last)
	}

	rec2 := doJSON(mux, "GET", "/api/v1/courses/nope/variants/go/challenges", nil)
	if rec2.Code != http.StatusNotFound {
		t.Errorf("unknown variant status = %d, want 404", rec2.Code)
	}
}
