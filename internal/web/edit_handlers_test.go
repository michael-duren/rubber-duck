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
		url.Values{"markdown": {seedMarkdown(t)}}, session)
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
