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
