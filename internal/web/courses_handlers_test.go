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
		{"no filter", "/courses", []string{"Intro to Go", "Build a Terminal", "// 2 courses"}, nil},
		// templ HTML-escapes the quoted query, hence &#34;.
		{"search by title", "/courses?q=terminal", []string{"Build a Terminal", "// 1 course matching &#34;terminal&#34;"}, []string{"Intro to Go"}},
		{"search is case-insensitive and hits languages", "/courses?q=GO", []string{"Intro to Go"}, []string{"Build a Terminal"}},
		{"search hits tags", "/courses?q=systems", []string{"Build a Terminal"}, []string{"Intro to Go"}},
		// "build-a-term" only matches the slug, not the spaced title.
		{"search hits slugs", "/courses?q=build-a-term", []string{"Build a Terminal"}, []string{"Intro to Go"}},
		{"tag filter", "/courses?tag=systems", []string{"Build a Terminal", "// 1 course --tag=systems"}, []string{"Intro to Go"}},
		// Multiple tag params narrow: a course must carry every selected tag.
		{"multi-tag keeps courses with all tags", "/courses?tag=systems&tag=cli", []string{"Build a Terminal", "// 1 course --tag=systems,cli"}, []string{"Intro to Go"}},
		{"multi-tag excludes partial matches", "/courses?tag=systems&tag=backend", []string{"no matches"}, []string{"Intro to Go", "Build a Terminal"}},
		{"lang filter", "/courses?lang=go", []string{"Intro to Go"}, []string{"Build a Terminal"}},
		{"combined filters exclude everything", "/courses?tag=systems&q=intro", []string{"no matches", "// 0 courses"}, []string{"Intro to Go", "Build a Terminal"}},
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

	rec := getPage(mux, "/courses?q=go", http.Header{"HX-Request": {"true"}})
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

	rec = getPage(mux, "/courses?q=go", http.Header{"HX-Request": {"true"}, "HX-History-Restore-Request": {"true"}})
	if !strings.Contains(rec.Body.String(), "<html") {
		t.Error("history restore should get the full page, got a fragment")
	}
}

// TestHomePage covers the landing page at "/": headline, signup CTA, and the
// featured-courses strip (capped at three, absent on an empty catalog).
func TestHomePage(t *testing.T) {
	t.Run("with courses", func(t *testing.T) {
		mux, fs := testMux(t)
		catalogFixture(fs)
		rec := getPage(mux, "/", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET / = %d, want 200", rec.Code)
		}
		body := rec.Body.String()
		for _, want := range []string{"Build it and see", `href="/signup"`, `href="/courses"`, "Intro to Go", "Build a Terminal"} {
			if !strings.Contains(body, want) {
				t.Errorf("GET /: body missing %q", want)
			}
		}
	})

	t.Run("empty catalog skips the featured strip", func(t *testing.T) {
		mux, _ := testMux(t)
		rec := getPage(mux, "/", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET / = %d, want 200", rec.Code)
		}
		if strings.Contains(rec.Body.String(), "fresh from the catalog") {
			t.Error("GET /: featured strip rendered with no courses")
		}
	})

	t.Run("featured strip caps at three", func(t *testing.T) {
		mux, fs := testMux(t)
		fs.courses = []domain.CourseSummary{
			{Slug: "a", Title: "Course A"}, {Slug: "b", Title: "Course B"},
			{Slug: "c", Title: "Course C"}, {Slug: "d", Title: "Course D"},
		}
		body := getPage(mux, "/", nil).Body.String()
		if !strings.Contains(body, "Course C") || strings.Contains(body, "Course D") {
			t.Error("GET /: featured strip should show the first three courses only")
		}
	})
}
