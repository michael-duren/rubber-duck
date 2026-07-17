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
	"github.com/michael-duren/rubber-duck/internal/ingest"
)

type fakeStore struct {
	versions map[string]int // "slug/lang" -> version
	courses  map[string]domain.Course
	sources  map[string]string
	variants map[string]domain.Variant

	users map[string]domain.User // token hash -> user, for UserByToken

	userByTokenErr error // if set, UserByToken fails with it (models the DB being down)

	proposals      map[int64]domain.Proposal
	nextProposalID int64
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		versions: map[string]int{}, courses: map[string]domain.Course{},
		sources: map[string]string{}, variants: map[string]domain.Variant{},
		users: map[string]domain.User{}, proposals: map[int64]domain.Proposal{},
	}
}

// UserByToken looks up a user by token hash — requireUser hashes before
// calling this, so the fake stores by that same hash to match (see addUser).
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

func (f *fakeStore) ListVariantSources(context.Context) ([]domain.VariantExport, error) {
	var out []domain.VariantExport
	for slug, c := range f.courses {
		for k, src := range f.sources {
			if lang, ok := strings.CutPrefix(k, slug+"/"); ok {
				out = append(out, domain.VariantExport{
					CourseSlug: c.Slug, Language: lang,
					Version: f.versions[k], SourceMD: src,
				})
			}
		}
	}
	return out, nil
}

func (f *fakeStore) ListTags(context.Context) ([]string, error) { return []string{"backend"}, nil }

// seedVariant loads a parsed course document into the fake's read-side maps,
// replacing what the old tests did via the deleted agent PUT endpoint.
func (f *fakeStore) seedVariant(t *testing.T, doc string) {
	t.Helper()
	src := []byte(doc)
	res, err := ingest.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	course, variant, err := ingest.ToDomain(res, src)
	if err != nil {
		t.Fatal(err)
	}
	k := f.key(course.Slug, variant.Language)
	f.courses[course.Slug] = course
	f.variants[k] = variant
	f.sources[k] = variant.SourceMD
	f.versions[k] = 1
}

// --- ProposalStore fake ---

func (f *fakeStore) CreateProposal(_ context.Context, proposerID int64, courseSlug, language, title, summary, markdown string) (domain.Proposal, error) {
	for _, p := range f.proposals {
		if p.ProposerID == proposerID && p.CourseSlug == courseSlug && p.Language == language && p.Status == domain.ProposalOpen {
			return domain.Proposal{}, domain.ErrDuplicateProposal
		}
	}
	f.nextProposalID++
	p := domain.Proposal{
		ID: f.nextProposalID, ProposerID: proposerID, CourseSlug: courseSlug,
		Language: language, Title: title, SummaryMD: summary, ProposedMD: markdown,
		BaseVersion: f.versions[f.key(courseSlug, language)],
		LiveVersion: f.versions[f.key(courseSlug, language)],
		Revision:    1, Status: domain.ProposalOpen,
	}
	f.proposals[p.ID] = p
	return p, nil
}

func (f *fakeStore) UpdateProposalMarkdown(_ context.Context, proposalID, proposerID int64, title, summary, markdown string) (domain.Proposal, error) {
	p, ok := f.proposals[proposalID]
	if !ok || p.ProposerID != proposerID {
		return domain.Proposal{}, domain.ErrNotFound
	}
	if p.Status != domain.ProposalOpen {
		return domain.Proposal{}, domain.ErrProposalClosed
	}
	p.Title, p.SummaryMD, p.ProposedMD = title, summary, markdown
	p.Revision++
	f.proposals[proposalID] = p
	return p, nil
}

func (f *fakeStore) ProposalByID(_ context.Context, id int64) (domain.Proposal, error) {
	p, ok := f.proposals[id]
	if !ok {
		return domain.Proposal{}, domain.ErrNotFound
	}
	return p, nil
}

func (f *fakeStore) ListProposalsByUser(_ context.Context, userID int64) ([]domain.Proposal, error) {
	var out []domain.Proposal
	for _, p := range f.proposals {
		if p.ProposerID == userID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeStore) WithdrawProposal(_ context.Context, proposalID, proposerID int64) error {
	p, ok := f.proposals[proposalID]
	if !ok || p.ProposerID != proposerID {
		return domain.ErrNotFound
	}
	if p.Status != domain.ProposalOpen {
		return domain.ErrProposalClosed
	}
	p.Status = domain.ProposalWithdrawn
	f.proposals[proposalID] = p
	return nil
}

// addUser registers a user token in the fake so a test can authenticate as
// that user via "Bearer "+token.
func (f *fakeStore) addUser(token string, user domain.User) {
	f.users[string(auth.HashToken(token))] = user
}

func testAPI(t *testing.T) (*http.ServeMux, *fakeStore) {
	t.Helper()
	mux := http.NewServeMux()
	fs := newFakeStore()
	Register(mux, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), fs, fs, fs)
	return mux, fs
}

// doJSON issues an unauthenticated request — the read API is public.
func doJSON(mux *http.ServeMux, method, path string, body any) *httptest.ResponseRecorder {
	return doJSONAs(mux, method, path, "", body)
}

// doJSONAs is doJSON with a caller-chosen bearer token ("" sends none).
func doJSONAs(mux *http.ServeMux, method, path, bearer string, body any) *httptest.ResponseRecorder {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rd)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
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

// TestPublicReads pins that the whole read API answers without any
// Authorization header: the duck CLI's pull/test flows and the repo-mirror
// export both depend on credential-free reads.
func TestPublicReads(t *testing.T) {
	mux, fs := testAPI(t)
	doc := seedDoc(t)
	fs.seedVariant(t, doc)

	rec := doJSON(mux, "GET", "/api/v1/courses", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("list courses = %d", rec.Code)
	}

	rec = doJSON(mux, "GET", "/api/v1/courses/intro-to-concurrency/variants/go", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get source = %d", rec.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal get response: %v", err)
	}
	if got["markdown"] != doc || got["version"].(float64) != 1 {
		t.Errorf("version = %v, markdown match = %t; want 1 and true", got["version"], got["markdown"] == doc)
	}

	if rec := doJSON(mux, "GET", "/api/v1/courses/nope/variants/go", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown variant = %d, want 404", rec.Code)
	}
	if rec := doJSON(mux, "GET", "/api/v1/tags", nil); rec.Code != http.StatusOK {
		t.Errorf("tags = %d", rec.Code)
	}
}

func TestExport(t *testing.T) {
	mux, fs := testAPI(t)
	doc := seedDoc(t)
	fs.seedVariant(t, doc)

	rec := doJSON(mux, "GET", "/api/v1/export", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("export = %d body %s", rec.Code, rec.Body)
	}
	var resp struct {
		Variants []struct {
			Course   string `json:"course"`
			Language string `json:"language"`
			Version  int    `json:"version"`
			Markdown string `json:"markdown"`
		} `json:"variants"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Variants) != 1 {
		t.Fatalf("variants = %d, want 1", len(resp.Variants))
	}
	v := resp.Variants[0]
	if v.Course != "intro-to-concurrency" || v.Language != "go" || v.Version != 1 || v.Markdown != doc {
		t.Errorf("export variant = %+v", v)
	}
}

// TestRequireUserStoreError covers auth-infrastructure failure (as opposed
// to a bad credential): a store error must surface as a logged 500, not a
// 401.
func TestRequireUserStoreError(t *testing.T) {
	var logBuf bytes.Buffer
	mux := http.NewServeMux()
	fs := newFakeStore()
	fs.userByTokenErr = errors.New("db down")
	Register(mux, slog.New(slog.NewTextHandler(&logBuf, nil)), fs, fs, fs)

	rec := doJSONAs(mux, "GET", "/api/v1/proposals", "gc_u_whatever", nil)
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

func TestListChallengesPublic(t *testing.T) {
	mux, fs := testAPI(t)
	fs.seedVariant(t, seedDoc(t))

	req := httptest.NewRequest("GET", "/api/v1/courses/intro-to-concurrency/variants/go/challenges", nil)
	rec := httptest.NewRecorder() // no Authorization header
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body)
	}

	var resp struct {
		Challenges []struct {
			LessonSlug   string `json:"lesson_slug"`
			LessonNumber int    `json:"lesson_number"`
			Slug         string `json:"slug"`
			Title        string `json:"title"`
			Points       int    `json:"points"`
			StarterCode  string `json:"starter_code"`
			TestCode     string `json:"test_code"`
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
	if n1, n2 := resp.Challenges[0].LessonNumber, resp.Challenges[1].LessonNumber; n1 != 1 || n2 != 2 {
		t.Errorf("lesson numbers = %d, %d, want 1, 2", n1, n2)
	}
	last := resp.Challenges[len(resp.Challenges)-1]
	if last.LessonSlug != "" || last.LessonNumber != 0 || last.Slug != "final" {
		t.Errorf("final challenge = %+v, want lesson_slug empty, lesson_number 0, slug final", last)
	}

	rec2 := doJSON(mux, "GET", "/api/v1/courses/nope/variants/go/challenges", nil)
	if rec2.Code != http.StatusNotFound {
		t.Errorf("unknown variant status = %d, want 404", rec2.Code)
	}
}

// TestRemovedAgentWriteEndpointsAreDead pins that the pre-proposal agent
// write surface stays gone: no /api/v1 route accepts course writes, with or
// without a credential. In this API-only mux an unmatched method is a 405
// (behind the full server the web catch-all 404s them); what matters is
// that nothing succeeds — a refactor re-registering a write handler under
// /api/v1/courses would fail this test.
func TestRemovedAgentWriteEndpointsAreDead(t *testing.T) {
	mux, fs := testAPI(t)
	fs.seedVariant(t, seedDoc(t))

	cases := []struct{ method, path string }{
		{"PUT", "/api/v1/courses/intro-to-concurrency/variants/go"},
		{"DELETE", "/api/v1/courses/intro-to-concurrency"},
		{"DELETE", "/api/v1/courses/intro-to-concurrency/variants/go"},
		{"POST", "/api/v1/courses"},
	}
	for _, c := range cases {
		rec := doJSONAs(mux, c.method, c.path, "gc_oldagentkey", map[string]string{"markdown": "x"})
		if rec.Code != http.StatusMethodNotAllowed && rec.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 405 or 404", c.method, c.path, rec.Code)
		}
	}
}
