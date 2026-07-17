package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/michael-duren/rubber-duck/internal/domain"
)

// seedMarkdown loads the canonical fixture (course: intro-to-concurrency,
// language: go) used across ingest tests as valid input.
func seedMarkdown(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile("../../seed/intro-to-go.md")
	if err != nil {
		t.Fatal(err)
	}
	return string(src)
}

// loginAlice signs up and returns the resulting session cookie.
func loginAlice(t *testing.T, mux *http.ServeMux) *http.Cookie {
	t.Helper()
	rec := postForm(mux, "/signup", url.Values{"username": {"alice"}, "password": {"supersecret"}}, nil)
	session := sessionFrom(rec)
	if session == nil {
		t.Fatal("signup did not set a session cookie")
	}
	return session
}

func TestEditVariantPageRequiresAuth(t *testing.T) {
	mux, _ := testMux(t)
	req := httptest.NewRequest("GET", "/courses/intro-to-concurrency/go/edit", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("anonymous GET /edit = %d, want 303 redirect to /login", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want /login", loc)
	}
}

// TestProposeVariantRequiresAuth covers the POST side of auth-gating that
// TestEditVariantPageRequiresAuth leaves untested: the GET page redirects an
// anonymous visitor, but the actual write path (proposeVariant) needs its
// own check, since it's the handler that would open an anonymous proposal
// if the route ever lost its h.requireUser wrapping.
func TestProposeVariantRequiresAuth(t *testing.T) {
	mux, fs := testMux(t)
	rec := postForm(mux, "/courses/intro-to-concurrency/go/edit",
		url.Values{"markdown": {"whatever"}}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("anonymous POST /edit = %d, want 303 redirect to /login", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want /login", loc)
	}
	if len(fs.proposals) != 0 {
		t.Errorf("anonymous POST must not open a proposal, got %d", len(fs.proposals))
	}
}

func TestEditVariantPageMissingVariant(t *testing.T) {
	mux, _ := testMux(t)
	session := loginAlice(t, mux)

	req := httptest.NewRequest("GET", "/courses/nope/go/edit", nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /edit for missing variant = %d, want 404", rec.Code)
	}
}

func TestEditVariantPagePrefillsMarkdown(t *testing.T) {
	mux, fs := testMux(t)
	session := loginAlice(t, mux)

	src := seedMarkdown(t)
	fs.variantSlug = "intro-to-concurrency"
	fs.variant = &fakeVariantGo
	fs.variantSource = src

	req := httptest.NewRequest("GET", "/courses/intro-to-concurrency/go/edit", nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /edit = %d, want 200: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "Introduction to Concurrency") {
		t.Error("edit page textarea should contain the stored markdown")
	}
}

func TestProposeVariantValidationErrorPreservesInput(t *testing.T) {
	mux, fs := testMux(t)
	session := loginAlice(t, mux)

	fs.variantSlug = "intro-to-concurrency"
	fs.variant = &fakeVariantGo
	fs.variantSource = seedMarkdown(t)

	badMarkdown := "this is not a valid course document at all"
	rec := postForm(mux, "/courses/intro-to-concurrency/go/edit",
		url.Values{"markdown": {badMarkdown}}, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST invalid markdown = %d, want 200 (re-render)", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "line") {
		t.Errorf("expected line-numbered problems in body, got: %s", body)
	}
	if !strings.Contains(body, badMarkdown) {
		t.Error("edit page should preserve exactly what the user submitted, not the old stored markdown")
	}
	if len(fs.proposals) != 0 {
		t.Errorf("invalid markdown must not open a proposal, got %d", len(fs.proposals))
	}
}

func TestProposeVariantSlugMismatchPreservesInput(t *testing.T) {
	mux, fs := testMux(t)
	session := loginAlice(t, mux)

	fs.variantSlug = "intro-to-concurrency"
	fs.variant = &fakeVariantGo
	fs.variantSource = seedMarkdown(t)

	// Valid document, but posted to a URL naming a different course slug.
	rec := postForm(mux, "/courses/some-other-course/go/edit",
		url.Values{"markdown": {seedMarkdown(t)}}, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST slug-mismatched markdown = %d, want 200 (re-render)", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "some-other-course") || !strings.Contains(body, "intro-to-concurrency") {
		t.Errorf("expected mismatch error naming both slugs, got: %s", body)
	}
	if !strings.Contains(body, "Introduction to Concurrency") {
		t.Error("edit page should preserve the submitted markdown on mismatch")
	}
	if len(fs.proposals) != 0 {
		t.Errorf("mismatched markdown must not open a proposal, got %d", len(fs.proposals))
	}
}

// TestProposeVariantOpensProposal is the core behavior change from the
// direct-edit era: submitting the editor never writes the variant — it
// opens a proposal for review and redirects to it, leaving the live
// content untouched until approvals publish it.
func TestProposeVariantOpensProposal(t *testing.T) {
	mux, fs := testMux(t)
	session := loginAlice(t, mux)

	fs.variantSlug = "intro-to-concurrency"
	fs.variant = &fakeVariantGo
	fs.variantSource = "live content"
	fs.variantVersion = 4

	rec := postForm(mux, "/courses/intro-to-concurrency/go/edit",
		url.Values{"markdown": {seedMarkdown(t)}, "title": {"Fix typos"}, "summary": {"spelling"}}, session)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST valid markdown = %d, want 303: %s", rec.Code, rec.Body)
	}
	if loc := rec.Header().Get("Location"); loc != "/proposals/1" {
		t.Errorf("Location = %q, want /proposals/1", loc)
	}

	if len(fs.proposals) != 1 {
		t.Fatalf("proposals = %d, want 1", len(fs.proposals))
	}
	p := fs.proposals[1]
	aliceID := fs.users["alice"].id
	if p.ProposerID != aliceID || p.CourseSlug != "intro-to-concurrency" || p.Language != "go" {
		t.Errorf("proposal = %+v", p)
	}
	if p.Title != "Fix typos" || p.SummaryMD != "spelling" || p.ProposedMD != seedMarkdown(t) {
		t.Errorf("proposal content = %q / %q", p.Title, p.SummaryMD)
	}
	if p.BaseVersion != 4 {
		t.Errorf("BaseVersion = %d, want the live version 4", p.BaseVersion)
	}
	// The live variant must be untouched: proposing is not publishing.
	if fs.variantSource != "live content" || fs.variantVersion != 4 {
		t.Error("proposing must not modify the live variant")
	}
}

// TestProposeVariantDuplicateRedirectsToExisting: a second propose for the
// same variant funnels into editing the existing open proposal rather than
// erroring or forking.
func TestProposeVariantDuplicateRedirectsToExisting(t *testing.T) {
	mux, fs := testMux(t)
	session := loginAlice(t, mux)

	fs.variantSlug = "intro-to-concurrency"
	fs.variant = &fakeVariantGo
	fs.variantSource = seedMarkdown(t)

	if rec := postForm(mux, "/courses/intro-to-concurrency/go/edit",
		url.Values{"markdown": {seedMarkdown(t)}}, session); rec.Code != http.StatusSeeOther {
		t.Fatalf("first propose = %d, want 303", rec.Code)
	}
	rec := postForm(mux, "/courses/intro-to-concurrency/go/edit",
		url.Values{"markdown": {seedMarkdown(t)}}, session)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("second propose = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/proposals/1/edit?dup=1" {
		t.Errorf("Location = %q, want /proposals/1/edit?dup=1", loc)
	}
	if len(fs.proposals) != 1 {
		t.Errorf("proposals = %d, want 1 (no fork)", len(fs.proposals))
	}
}

// --- issue #38: creating a brand-new course / language variant ---

// TestNewCoursePageRequiresAuth mirrors TestEditVariantPageRequiresAuth: the
// entry-point form is behind h.requireUser same as the editor it feeds.
func TestNewCoursePageRequiresAuth(t *testing.T) {
	mux, _ := testMux(t)
	req := httptest.NewRequest("GET", "/courses/new", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("anonymous GET /courses/new = %d, want 303 redirect to /login", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want /login", loc)
	}
}

// TestCreateCourseRequiresAuth is the POST-side counterpart of
// TestNewCoursePageRequiresAuth: the form-viewing GET redirects an anonymous
// visitor, but createCourse (the handler that actually redirects into the
// editor and, on the following save, creates the course) needs its own
// auth-gating check too.
func TestCreateCourseRequiresAuth(t *testing.T) {
	mux, _ := testMux(t)
	rec := postForm(mux, "/courses/new",
		url.Values{"slug": {"brand-new-course"}, "title": {"T"}, "language": {"go"}}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("anonymous POST /courses/new = %d, want 303 redirect to /login", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want /login", loc)
	}
}

// TestNewCoursePageRenders covers the plain, un-prefilled form (the
// catalog's "+ New course" link) and the pre-filled variant (an existing
// course's "+ Add language variant" link, which arrives with slug/title in
// the query string).
func TestNewCoursePageRenders(t *testing.T) {
	mux, _ := testMux(t)
	session := loginAlice(t, mux)

	req := httptest.NewRequest("GET", "/courses/new", nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /courses/new = %d, want 200: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{`name="slug"`, `name="title"`, `name="language"`, `name="description"`, `action="/courses/new"`} {
		if !strings.Contains(body, want) {
			t.Errorf("expected form to contain %q, got: %s", want, body)
		}
	}

	req = httptest.NewRequest("GET", "/courses/new?slug=intro-to-concurrency&title=Introduction+to+Concurrency&description=A+pitch", nil)
	req.AddCookie(session)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /courses/new (prefilled) = %d, want 200: %s", rec.Code, rec.Body)
	}
	body = rec.Body.String()
	for _, want := range []string{"intro-to-concurrency", "Introduction to Concurrency", "A pitch"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected prefilled form to contain %q, got: %s", want, body)
		}
	}
}

// TestCreateCourseValidation covers createCourse rejecting a bad submit
// (missing fields, an invalid slug, an unsupported language) by re-rendering
// the form with an error rather than redirecting into the editor.
func TestCreateCourseValidation(t *testing.T) {
	cases := []struct {
		name           string
		form           url.Values
		wantErrsnippet string
	}{
		{"missing slug", url.Values{"title": {"T"}, "language": {"go"}}, "required"},
		{"missing title", url.Values{"slug": {"my-course"}, "language": {"go"}}, "required"},
		{"missing language", url.Values{"slug": {"my-course"}, "title": {"T"}}, "required"},
		{"bad slug", url.Values{"slug": {"My Course!"}, "title": {"T"}, "language": {"go"}}, "lowercase"},
		{"bad language", url.Values{"slug": {"my-course"}, "title": {"T"}, "language": {"rust"}}, "must be one of"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mux, _ := testMux(t)
			session := loginAlice(t, mux)
			rec := postForm(mux, "/courses/new", c.form, session)
			if rec.Code != http.StatusOK {
				t.Fatalf("POST /courses/new = %d, want 200 (re-render): %s", rec.Code, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), c.wantErrsnippet) {
				t.Errorf("expected error containing %q, got: %s", c.wantErrsnippet, rec.Body.String())
			}
		})
	}
}

// TestCreateCourseRedirectsIntoEditor is the success path: a valid submit
// redirects (303) into the editor at the entered slug/language, with new=1
// so editVariantPage seeds a template instead of 404ing.
func TestCreateCourseRedirectsIntoEditor(t *testing.T) {
	mux, _ := testMux(t)
	session := loginAlice(t, mux)

	rec := postForm(mux, "/courses/new",
		url.Values{"slug": {"brand-new-course"}, "title": {"Brand New Course"}, "language": {"go"}, "description": {"A pitch."}}, session)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /courses/new = %d, want 303: %s", rec.Code, rec.Body)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/courses/brand-new-course/go/edit?") {
		t.Fatalf("Location = %q, want /courses/brand-new-course/go/edit?...", loc)
	}
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("new") != "1" {
		t.Errorf("redirect query missing new=1: %q", loc)
	}
	if q.Get("title") != "Brand New Course" || q.Get("description") != "A pitch." {
		t.Errorf("redirect query should carry title/description through, got: %q", loc)
	}
}

// TestEditVariantPageNewSeedsTemplate covers editVariantPage's new=1 branch:
// a nonexistent slug/lang, reached with new=1 (as createCourse's redirect
// does), renders a seeded template instead of 404ing.
func TestEditVariantPageNewSeedsTemplate(t *testing.T) {
	mux, _ := testMux(t)
	session := loginAlice(t, mux)

	req := httptest.NewRequest("GET", "/courses/brand-new-course/go/edit?new=1&title=Brand+New+Course&description=A+pitch.", nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET .../edit?new=1 for missing variant = %d, want 200: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{"course: brand-new-course", "title: Brand New Course", "language: go", "description: A pitch.", "# Lesson:", "# Final Challenge:"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected seeded template to contain %q, got: %s", want, body)
		}
	}
}

// TestEditVariantPageWithoutNewStill404s is the regression guard called out
// by issue #38: without new=1, a nonexistent slug/lang must still 404 —
// don't let every mistyped URL become an accidental "create" page.
func TestEditVariantPageWithoutNewStill404s(t *testing.T) {
	mux, _ := testMux(t)
	session := loginAlice(t, mux)

	req := httptest.NewRequest("GET", "/courses/brand-new-course/go/edit", nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET .../edit (no new=1) for missing variant = %d, want 404", rec.Code)
	}
}

// TestProposeVariantNewCourse is the end-to-end check of the new-course
// flow: submitting the seeded template opens a proposal with base version 0
// (the variant doesn't exist), via the ordinary proposeVariant path.
func TestProposeVariantNewCourse(t *testing.T) {
	mux, fs := testMux(t)
	session := loginAlice(t, mux)

	tmpl := newVariantTemplate("brand-new-course", "go", "Brand New Course", "A pitch.")
	rec := postForm(mux, "/courses/brand-new-course/go/edit",
		url.Values{"markdown": {tmpl}}, session)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST seeded template = %d, want 303 (proposed): %s", rec.Code, rec.Body)
	}
	if loc := rec.Header().Get("Location"); loc != "/proposals/1" {
		t.Errorf("Location = %q, want /proposals/1", loc)
	}
	if len(fs.proposals) != 1 {
		t.Fatalf("proposals = %d, want 1", len(fs.proposals))
	}
	p := fs.proposals[1]
	if p.CourseSlug != "brand-new-course" || p.Language != "go" || p.BaseVersion != 0 {
		t.Errorf("proposal = %+v, want brand-new-course/go at base 0", p)
	}
	// An empty title falls back to the generated default.
	if !strings.Contains(p.Title, "brand-new-course/go") {
		t.Errorf("default title = %q", p.Title)
	}
}

// fakeVariantGo is a minimal domain.Variant satisfying fakeStore's
// VariantDetail/VariantSource slug+language matching for the "go" variant of
// intro-to-concurrency; only Language is consulted by the fake.
var fakeVariantGo = domain.Variant{Language: "go"}

// TestCreateCourseExistingSlugShowsStoredContent guards against a future
// regression in the new=1 flow: createCourse blindly redirects into the
// editor with new=1 for whatever slug/language was submitted — it doesn't
// itself check whether a variant already exists there — so the only thing
// standing between this "New course" form and silently blanking existing
// content is editVariantPage's own VariantSource lookup (its new=1 branch
// only fires on domain.ErrNotFound; an existing variant falls through to the
// ordinary render with the real stored markdown). This exercises that
// end-to-end: "creating" a course at a slug/language fakeStore already has a
// variant for must land on the existing stored markdown, never the blank
// newVariantTemplate.
func TestCreateCourseExistingSlugShowsStoredContent(t *testing.T) {
	mux, fs := testMux(t)
	session := loginAlice(t, mux)

	fs.variantSlug = "intro-to-concurrency"
	fs.variant = &fakeVariantGo
	fs.variantSource = seedMarkdown(t)

	rec := postForm(mux, "/courses/new",
		url.Values{"slug": {"intro-to-concurrency"}, "title": {"Introduction to Concurrency"}, "language": {"go"}, "description": {"A pitch."}}, session)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /courses/new (existing slug/lang) = %d, want 303: %s", rec.Code, rec.Body)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/courses/intro-to-concurrency/go/edit?") {
		t.Fatalf("Location = %q, want /courses/intro-to-concurrency/go/edit?...", loc)
	}

	req := httptest.NewRequest("GET", loc, nil)
	req.AddCookie(session)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200: %s", loc, rec.Code, rec.Body)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Introduction to Concurrency") {
		t.Errorf("expected the existing stored markdown, got: %s", body)
	}
	if strings.Contains(body, "TODO: write this lesson's content.") {
		t.Error("an existing variant's content must not be replaced by the blank new-course template")
	}
	if len(fs.proposals) != 0 {
		t.Errorf("this flow must not open a proposal, got %d", len(fs.proposals))
	}
}

// --- issue #39: live preview ---

// TestPreviewMarkdownRequiresAuth mirrors TestEditVariantPageRequiresAuth:
// the preview endpoint is gated exactly like the editors it serves, so an
// anonymous caller never gets a rendering oracle.
func TestPreviewMarkdownRequiresAuth(t *testing.T) {
	mux, _ := testMux(t)
	rec := postForm(mux, "/preview/markdown", url.Values{"markdown": {"# Hi"}}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("anonymous POST /preview/markdown = %d, want 303 redirect to /login", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want /login", loc)
	}
}

// TestPreviewMarkdownStripsFrontmatterAndRendersMarkdown covers the core
// behavior: a full course document (frontmatter + a fenced code block) POSTed
// to the preview endpoint comes back as HTML with the frontmatter gone and
// the markdown structure (heading, code block) actually rendered — not the
// raw frontmatter keys, and no side effects (no proposal opened).
func TestPreviewMarkdownStripsFrontmatterAndRendersMarkdown(t *testing.T) {
	mux, fs := testMux(t)
	session := loginAlice(t, mux)

	doc := "---\n" +
		"course: intro-to-concurrency\n" +
		"title: Introduction to Concurrency\n" +
		"language: go\n" +
		"description: A course.\n" +
		"---\n\n" +
		"# Lesson: Getting Started {#getting-started}\n\n" +
		"Some prose about goroutines.\n\n" +
		"```go\n" +
		"func main() {}\n" +
		"```\n"

	rec := postForm(mux, "/preview/markdown", url.Values{"markdown": {doc}}, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /preview/markdown = %d, want 200: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	if strings.Contains(body, "course:") || strings.Contains(body, "language:") {
		t.Errorf("preview HTML should not contain raw frontmatter keys, got: %s", body)
	}
	if !strings.Contains(body, "<h1") {
		t.Errorf("expected a rendered heading, got: %s", body)
	}
	if !strings.Contains(body, "<pre") && !strings.Contains(body, "<code") {
		t.Errorf("expected a rendered code block, got: %s", body)
	}
	if len(fs.proposals) != 0 {
		t.Errorf("preview must be side-effect-free, got %d proposals", len(fs.proposals))
	}
}
