package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
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

// TestSaveVariantRequiresAuth covers the POST side of auth-gating that
// TestEditVariantPageRequiresAuth leaves untested: the GET page redirects an
// anonymous visitor, but the actual write path (saveVariant) needs its own
// check, since it's the handler that would persist an anonymous edit if the
// route ever lost its h.requireUser wrapping.
func TestSaveVariantRequiresAuth(t *testing.T) {
	mux, fs := testMux(t)
	rec := postForm(mux, "/courses/intro-to-concurrency/go/edit",
		url.Values{"markdown": {"whatever"}, "version": {"0"}}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("anonymous POST /edit = %d, want 303 redirect to /login", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want /login", loc)
	}
	if len(fs.upserts) != 0 {
		t.Errorf("anonymous POST must not save, got %d upserts", len(fs.upserts))
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

func TestSaveVariantValidationErrorPreservesInput(t *testing.T) {
	mux, fs := testMux(t)
	session := loginAlice(t, mux)

	fs.variantSlug = "intro-to-concurrency"
	fs.variant = &fakeVariantGo
	fs.variantSource = seedMarkdown(t)

	badMarkdown := "this is not a valid course document at all"
	rec := postForm(mux, "/courses/intro-to-concurrency/go/edit",
		url.Values{"markdown": {badMarkdown}, "version": {"0"}}, session)
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
	if len(fs.upserts) != 0 {
		t.Errorf("invalid markdown must not be saved, got %d upserts", len(fs.upserts))
	}
}

func TestSaveVariantSlugMismatchPreservesInput(t *testing.T) {
	mux, fs := testMux(t)
	session := loginAlice(t, mux)

	fs.variantSlug = "intro-to-concurrency"
	fs.variant = &fakeVariantGo
	fs.variantSource = seedMarkdown(t)

	// Valid document, but posted to a URL naming a different course slug.
	rec := postForm(mux, "/courses/some-other-course/go/edit",
		url.Values{"markdown": {seedMarkdown(t)}, "version": {"0"}}, session)
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
	if len(fs.upserts) != 0 {
		t.Errorf("mismatched markdown must not be saved, got %d upserts", len(fs.upserts))
	}
}

func TestSaveVariantSuccessRedirectsAndAttributesEditor(t *testing.T) {
	mux, fs := testMux(t)
	session := loginAlice(t, mux)

	fs.variantSlug = "intro-to-concurrency"
	fs.variant = &fakeVariantGo
	fs.variantSource = "stale"

	rec := postForm(mux, "/courses/intro-to-concurrency/go/edit",
		url.Values{"markdown": {seedMarkdown(t)}, "version": {"0"}}, session)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST valid markdown = %d, want 303: %s", rec.Code, rec.Body)
	}
	if loc := rec.Header().Get("Location"); loc != "/courses/intro-to-concurrency/go" {
		t.Errorf("Location = %q, want /courses/intro-to-concurrency/go", loc)
	}

	if len(fs.upserts) != 1 {
		t.Fatalf("upserts = %d, want 1", len(fs.upserts))
	}
	call := fs.upserts[0]
	if call.Course.Slug != "intro-to-concurrency" || call.Variant.Language != "go" {
		t.Errorf("upserted course/variant = %+v / %+v", call.Course, call.Variant)
	}
	if call.EditedBy == nil {
		t.Fatal("EditedBy must be set for a web-form save")
	}
	aliceID := fs.users["alice"].id
	if *call.EditedBy != aliceID {
		t.Errorf("EditedBy = %d, want %d (alice)", *call.EditedBy, aliceID)
	}
	// The redirect and attribution checks above don't confirm the actual
	// document content was persisted — assert that separately: the fake
	// store's variantSource is only updated by UpsertVariant on success
	// (see fakeStore.UpsertVariant), so this is a genuine "stale" ->
	// "newly-submitted markdown" read-after-write check, not a tautology.
	if fs.variantSource != seedMarkdown(t) {
		t.Error("stored markdown should be updated to exactly what was submitted")
	}
	if call.Variant.SourceMD != seedMarkdown(t) {
		t.Error("UpsertVariant should be called with the submitted markdown, not the stale stored one")
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

// TestSaveVariantCreatesNewCourseNoFriction is the end-to-end check of
// issue #38's core claim: saving the seeded template as-is creates the
// course + variant via the ordinary saveVariant path, with no version
// conflict and no destructive-republish confirmation (there's nothing to
// conflict with or overwrite yet).
func TestSaveVariantCreatesNewCourseNoFriction(t *testing.T) {
	mux, fs := testMux(t)
	session := loginAlice(t, mux)

	tmpl := newVariantTemplate("brand-new-course", "go", "Brand New Course", "A pitch.")
	rec := postForm(mux, "/courses/brand-new-course/go/edit",
		url.Values{"markdown": {tmpl}, "version": {"0"}}, session)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST seeded template = %d, want 303 (saved): %s", rec.Code, rec.Body)
	}
	if loc := rec.Header().Get("Location"); loc != "/courses/brand-new-course/go" {
		t.Errorf("Location = %q, want /courses/brand-new-course/go", loc)
	}
	if len(fs.upserts) != 1 {
		t.Fatalf("upserts = %d, want 1", len(fs.upserts))
	}
	call := fs.upserts[0]
	if call.Course.Slug != "brand-new-course" || call.Variant.Language != "go" {
		t.Errorf("upserted course/variant = %+v / %+v", call.Course, call.Variant)
	}
}

// TestSaveVariantCreatesNewCourseWithMinimalEdits is the same flow, but with
// the TODO placeholders filled in first — the point of the seeded template
// is that it's a starting point, not necessarily the final document.
func TestSaveVariantCreatesNewCourseWithMinimalEdits(t *testing.T) {
	mux, fs := testMux(t)
	session := loginAlice(t, mux)

	tmpl := newVariantTemplate("brand-new-course", "python", "Brand New Course", "A pitch.")
	edited := strings.Replace(tmpl, "TODO: write this lesson's content.", "Here's the real lesson content.", 1)
	rec := postForm(mux, "/courses/brand-new-course/python/edit",
		url.Values{"markdown": {edited}, "version": {"0"}}, session)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST edited seeded template = %d, want 303 (saved): %s", rec.Code, rec.Body)
	}
	if len(fs.upserts) != 1 {
		t.Fatalf("upserts = %d, want 1", len(fs.upserts))
	}
}

// fakeVariantGo is a minimal domain.Variant satisfying fakeStore's
// VariantDetail/VariantSource slug+language matching for the "go" variant of
// intro-to-concurrency; only Language is consulted by the fake.
var fakeVariantGo = domain.Variant{Language: "go"}

// TestSaveVariantVersionConflictPreservesInput covers issue #36: someone
// else's save landed between this page being loaded (at version 1) and this
// submit, so the fake store's current version has already moved to 2. The
// stale submit must be rejected with a clear message, must not silently
// refetch/merge, and — like every other rejected save — must preserve
// exactly what the user typed rather than reverting to stored content.
func TestSaveVariantVersionConflictPreservesInput(t *testing.T) {
	mux, fs := testMux(t)
	session := loginAlice(t, mux)

	fs.variantSlug = "intro-to-concurrency"
	fs.variant = &fakeVariantGo
	fs.variantSource = seedMarkdown(t)
	fs.variantVersion = 2 // someone else already saved after this page was loaded at version 1

	edited := strings.Replace(seedMarkdown(t), "Introduction to Concurrency", "Introduction to Concurrency (edited)", 1)
	rec := postForm(mux, "/courses/intro-to-concurrency/go/edit",
		url.Values{"markdown": {edited}, "version": {"1"}}, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST stale version = %d, want 200 (re-render): %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Someone else changed this since you opened it") {
		t.Errorf("expected version-conflict message, got: %s", body)
	}
	if !strings.Contains(body, "(edited)") {
		t.Error("edit page should preserve exactly what the user submitted on a version conflict")
	}
	if len(fs.upserts) != 0 {
		t.Errorf("stale write must not be applied, got %d upserts", len(fs.upserts))
	}
	if fs.variantSource != seedMarkdown(t) {
		t.Error("stored variant must be untouched by the rejected stale write")
	}
}

// TestSaveVariantMissingVersionPreservesInput covers a malformed/tampered
// submit missing the hidden version field: it must be rejected (there's no
// version to safely check against) rather than treated as an unconditional
// overwrite, while still preserving the user's submitted text.
func TestSaveVariantMissingVersionPreservesInput(t *testing.T) {
	mux, fs := testMux(t)
	session := loginAlice(t, mux)

	fs.variantSlug = "intro-to-concurrency"
	fs.variant = &fakeVariantGo
	fs.variantSource = seedMarkdown(t)

	rec := postForm(mux, "/courses/intro-to-concurrency/go/edit",
		url.Values{"markdown": {seedMarkdown(t)}}, session) // no "version" field
	if rec.Code != http.StatusOK {
		t.Fatalf("POST missing version = %d, want 200 (re-render): %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "Missing or invalid version") {
		t.Errorf("expected missing-version message, got: %s", rec.Body.String())
	}
	if len(fs.upserts) != 0 {
		t.Errorf("write without a version must not be applied, got %d upserts", len(fs.upserts))
	}
}

// TestEditVariantPageShowsSubmissionWarning covers issue #37's GET side: a
// variant with existing submissions shows the exact count as soon as the
// page loads, before the user ever clicks Save.
func TestEditVariantPageShowsSubmissionWarning(t *testing.T) {
	mux, fs := testMux(t)
	session := loginAlice(t, mux)

	fs.variantSlug = "intro-to-concurrency"
	fs.variant = &fakeVariantGo
	fs.variantSource = seedMarkdown(t)
	fs.subCount = 3

	req := httptest.NewRequest("GET", "/courses/intro-to-concurrency/go/edit", nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /edit = %d, want 200: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "3 submission") {
		t.Errorf("expected the exact submission count in the warning, got: %s", body)
	}
	if !strings.Contains(body, "Yes, save anyway") {
		t.Error("expected the confirming submit control when submissions exist")
	}
	// The "Yes, save anyway" button is only meaningful if the hidden field it
	// depends on (see views.EditVariant/edit.templ) is actually present —
	// assert on the markup (via a regex tolerant of templ's exact
	// whitespace/self-closing-slash codegen) so a future template edit that
	// drops the hidden field, while leaving the button's visible text
	// intact, fails a test instead of silently breaking every
	// destructive-save confirmation.
	if !confirmedHiddenFieldPattern.MatchString(body) {
		t.Error("expected the hidden confirmed=1 field the 'Yes, save anyway' button depends on")
	}
}

// confirmedHiddenFieldPattern matches an <input type="hidden" ...> tag that
// also carries name="confirmed" and value="1", in either attribute order,
// without depending on templ's exact whitespace or self-closing-slash
// codegen (see edit_templ.go, which renders it without a trailing "/").
var confirmedHiddenFieldPattern = regexp.MustCompile(
	`<input[^>]*type="hidden"[^>]*name="confirmed"[^>]*value="1"[^>]*>|<input[^>]*type="hidden"[^>]*value="1"[^>]*name="confirmed"[^>]*>`)

// TestSaveVariantNoSubmissionsSavesImmediately is the non-regression case:
// a variant with zero submissions saves on the first plain Save, with no
// confirmation step, exactly like before issue #37.
func TestSaveVariantNoSubmissionsSavesImmediately(t *testing.T) {
	mux, fs := testMux(t)
	session := loginAlice(t, mux)

	fs.variantSlug = "intro-to-concurrency"
	fs.variant = &fakeVariantGo
	fs.variantSource = "stale"
	fs.subCount = 0

	rec := postForm(mux, "/courses/intro-to-concurrency/go/edit",
		url.Values{"markdown": {seedMarkdown(t)}, "version": {"0"}}, session)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST with no submissions = %d, want 303 (saved immediately): %s", rec.Code, rec.Body)
	}
	if len(fs.upserts) != 1 {
		t.Fatalf("upserts = %d, want 1", len(fs.upserts))
	}
}

// TestSaveVariantWithSubmissionsRequiresConfirmation covers issue #37's core
// case: a variant with existing submissions, submitted without the
// confirmed=1 field, must not save — it re-renders the edit page instead,
// naming the exact count and preserving exactly what the user typed.
func TestSaveVariantWithSubmissionsRequiresConfirmation(t *testing.T) {
	mux, fs := testMux(t)
	session := loginAlice(t, mux)

	fs.variantSlug = "intro-to-concurrency"
	fs.variant = &fakeVariantGo
	fs.variantSource = seedMarkdown(t)
	fs.subCount = 5

	edited := strings.Replace(seedMarkdown(t), "Introduction to Concurrency", "Introduction to Concurrency (in progress)", 1)
	rec := postForm(mux, "/courses/intro-to-concurrency/go/edit",
		url.Values{"markdown": {edited}, "version": {"0"}}, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST unconfirmed with submissions = %d, want 200 (re-render): %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "5 submission") {
		t.Errorf("expected the exact submission count in the warning, got: %s", body)
	}
	if !strings.Contains(body, "(in progress)") {
		t.Error("edit page should preserve exactly what the user submitted while awaiting confirmation")
	}
	if len(fs.upserts) != 0 {
		t.Errorf("unconfirmed destructive save must not be applied, got %d upserts", len(fs.upserts))
	}
}

// TestSaveVariantVersionConflictWinsOverSubmissionConfirmation covers the
// composition of issue #36 (version conflict) and issue #37 (submission
// confirmation) when both apply to the same submit: saveVariant's doc
// comment says the version-conflict peek runs, and wins, before the
// submission-count check, so a stale submit against a variant that also has
// outstanding submissions must show the version-conflict message — not the
// submission-count warning — and must not save either way.
func TestSaveVariantVersionConflictWinsOverSubmissionConfirmation(t *testing.T) {
	mux, fs := testMux(t)
	session := loginAlice(t, mux)

	fs.variantSlug = "intro-to-concurrency"
	fs.variant = &fakeVariantGo
	fs.variantSource = seedMarkdown(t)
	fs.variantVersion = 2 // someone else already saved after this page was loaded at version 1
	fs.subCount = 5       // ...and there are outstanding submissions on top of that

	edited := strings.Replace(seedMarkdown(t), "Introduction to Concurrency", "Introduction to Concurrency (edited)", 1)
	rec := postForm(mux, "/courses/intro-to-concurrency/go/edit",
		url.Values{"markdown": {edited}, "version": {"1"}}, session) // stale version, no confirmed=1
	if rec.Code != http.StatusOK {
		t.Fatalf("POST stale version with submissions = %d, want 200 (re-render): %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Someone else changed this since you opened it") {
		t.Errorf("expected the version-conflict message to win, got: %s", body)
	}
	if strings.Contains(body, "5 submission") || strings.Contains(body, "Yes, save anyway") {
		t.Error("submission-count warning should not appear once the version conflict already applies")
	}
	if len(fs.upserts) != 0 {
		t.Errorf("neither check should let a save through, got %d upserts", len(fs.upserts))
	}
}

// TestSaveVariantWithSubmissionsConfirmedSaves is the other half of the same
// case: the same submit, but with confirmed=1 present (as the rendered "Yes,
// save anyway" button sends), must proceed and save.
func TestSaveVariantWithSubmissionsConfirmedSaves(t *testing.T) {
	mux, fs := testMux(t)
	session := loginAlice(t, mux)

	fs.variantSlug = "intro-to-concurrency"
	fs.variant = &fakeVariantGo
	fs.variantSource = seedMarkdown(t)
	fs.subCount = 5

	rec := postForm(mux, "/courses/intro-to-concurrency/go/edit",
		url.Values{"markdown": {seedMarkdown(t)}, "version": {"0"}, "confirmed": {"1"}}, session)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST confirmed with submissions = %d, want 303 (saved): %s", rec.Code, rec.Body)
	}
	if len(fs.upserts) != 1 {
		t.Fatalf("upserts = %d, want 1", len(fs.upserts))
	}
}

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
	if len(fs.upserts) != 0 {
		t.Errorf("this flow must not save anything, got %d upserts", len(fs.upserts))
	}
}

// --- issue #39: live preview ---

// TestPreviewVariantRequiresAuth mirrors TestEditVariantPageRequiresAuth:
// the preview endpoint is gated exactly like the editor page it serves, so
// an anonymous caller never gets a rendering oracle.
func TestPreviewVariantRequiresAuth(t *testing.T) {
	mux, _ := testMux(t)
	rec := postForm(mux, "/courses/intro-to-concurrency/go/edit/preview", url.Values{"markdown": {"# Hi"}}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("anonymous POST /edit/preview = %d, want 303 redirect to /login", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want /login", loc)
	}
}

// TestPreviewVariantStripsFrontmatterAndRendersMarkdown covers the core
// behavior: a full course document (frontmatter + a fenced code block) POSTed
// to the preview endpoint comes back as HTML with the frontmatter gone and
// the markdown structure (heading, code block) actually rendered — not the
// raw frontmatter keys, and not a real save (no store upsert).
func TestPreviewVariantStripsFrontmatterAndRendersMarkdown(t *testing.T) {
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

	rec := postForm(mux, "/courses/intro-to-concurrency/go/edit/preview", url.Values{"markdown": {doc}}, session)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /edit/preview = %d, want 200: %s", rec.Code, rec.Body)
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
	if len(fs.upserts) != 0 {
		t.Errorf("preview must be side-effect-free, got %d upserts", len(fs.upserts))
	}
}
