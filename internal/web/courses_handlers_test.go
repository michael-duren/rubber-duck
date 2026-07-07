package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/michael-duren/rubber-duck/internal/domain"
)

// catalogFixture seeds two courses distinct enough that every filter axis
// (q, tag, lang) can isolate one of them.
func catalogFixture(f *fakeStore) {
	f.courses = []domain.CourseSummary{
		{Slug: "intro-to-go", Title: "Intro to Go", Tags: []string{"backend"}, Languages: []string{"go"}},
		{Slug: "build-a-terminal", Title: "Build a Terminal", Tags: []string{"systems", "cli"}, Languages: []string{"c", "rust"}},
	}
}

func getPage(mux *http.ServeMux, target string, header http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", target, nil)
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestCatalogFiltering(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		want    []string // page must contain all of these
		exclude []string // and none of these
	}{
		{"no filter", "/", []string{"Intro to Go", "Build a Terminal", "// 2 courses"}, nil},
		// templ HTML-escapes the quoted query, hence &#34;.
		{"search by title", "/?q=terminal", []string{"Build a Terminal", "// 1 course matching &#34;terminal&#34;"}, []string{"Intro to Go"}},
		{"search is case-insensitive and hits languages", "/?q=GO", []string{"Intro to Go"}, []string{"Build a Terminal"}},
		{"search hits tags", "/?q=systems", []string{"Build a Terminal"}, []string{"Intro to Go"}},
		// "build-a-term" only matches the slug, not the spaced title.
		{"search hits slugs", "/?q=build-a-term", []string{"Build a Terminal"}, []string{"Intro to Go"}},
		{"tag filter", "/?tag=systems", []string{"Build a Terminal", "// 1 course --tag=systems"}, []string{"Intro to Go"}},
		// Multiple tag params narrow: a course must carry every selected tag.
		{"multi-tag keeps courses with all tags", "/?tag=systems&tag=cli", []string{"Build a Terminal", "// 1 course --tag=systems,cli"}, []string{"Intro to Go"}},
		{"multi-tag excludes partial matches", "/?tag=systems&tag=backend", []string{"no matches"}, []string{"Intro to Go", "Build a Terminal"}},
		{"lang filter", "/?lang=go", []string{"Intro to Go"}, []string{"Build a Terminal"}},
		{"combined filters exclude everything", "/?tag=systems&q=intro", []string{"no matches", "// 0 courses"}, []string{"Intro to Go", "Build a Terminal"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux, fs := testMux(t)
			catalogFixture(fs)
			rec := getPage(mux, tt.target, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", tt.target, rec.Code)
			}
			body := rec.Body.String()
			for _, want := range tt.want {
				if !strings.Contains(body, want) {
					t.Errorf("GET %s: body missing %q", tt.target, want)
				}
			}
			for _, excl := range tt.exclude {
				if strings.Contains(body, excl) {
					t.Errorf("GET %s: body unexpectedly contains %q", tt.target, excl)
				}
			}
		})
	}
}

// TestCatalogHTMXFragment covers the live-search contract: an HX-Request gets
// only the #catalog-results fragment, while a history restore (which htmx
// also marks HX-Request) must get the full page back.
func TestCatalogHTMXFragment(t *testing.T) {
	mux, fs := testMux(t)
	catalogFixture(fs)

	rec := getPage(mux, "/?q=go", http.Header{"HX-Request": {"true"}})
	body := rec.Body.String()
	if strings.Contains(body, "<html") {
		t.Error("HX-Request response should be a fragment, got a full page")
	}
	if !strings.Contains(body, `id="catalog-results"`) {
		t.Error("fragment missing catalog-results swap target")
	}
	if !strings.Contains(body, "Intro to Go") {
		t.Error("fragment missing filtered course")
	}

	rec = getPage(mux, "/?q=go", http.Header{"HX-Request": {"true"}, "HX-History-Restore-Request": {"true"}})
	if !strings.Contains(rec.Body.String(), "<html") {
		t.Error("history restore should get the full page, got a fragment")
	}
}
