package web

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/michael-duren/rubber-duck/internal/domain"
)

// --- fakeStore's learning-path slice (fields in auth_handlers_test.go) ---

func (f *fakeStore) ListPaths(context.Context) ([]domain.PathSummary, error) {
	return f.paths, nil
}

func (f *fakeStore) PathBySlug(_ context.Context, slug string) (domain.LearningPath, []domain.CourseSummary, error) {
	if f.path == nil || f.path.Slug != slug {
		return domain.LearningPath{}, nil, domain.ErrNotFound
	}
	return *f.path, f.pathCourses, nil
}

func pathFixture(f *fakeStore) {
	f.path = &domain.LearningPath{
		Slug:            "go-developer",
		Title:           "Go Developer",
		DescriptionHTML: "<p>From zero to production Go.</p>",
		OverviewHTML:    "<h2>Why this order</h2>",
		CourseSlugs:     []string{"go-basics", "intro-to-concurrency"},
	}
	f.pathCourses = []domain.CourseSummary{
		{Slug: "go-basics", Title: "Go Basics", Tags: []string{"go"}, Languages: []string{"go"}, DurationHours: 8},
		{Slug: "intro-to-concurrency", Title: "Introduction to Concurrency", Languages: []string{"go", "python"}, DurationHours: 6},
	}
	f.paths = []domain.PathSummary{{
		Slug: "go-developer", Title: "Go Developer",
		DescriptionHTML: "<p>From zero to production Go.</p>", CourseCount: 2,
	}}
}

func TestPathsIndex(t *testing.T) {
	t.Run("lists published paths", func(t *testing.T) {
		mux, fs := testMux(t)
		pathFixture(fs)
		rec := getPage(mux, "/paths", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /paths = %d", rec.Code)
		}
		for _, want := range []string{"Go Developer", "From zero to production Go.", "2 courses", "/paths/go-developer"} {
			if !strings.Contains(rec.Body.String(), want) {
				t.Errorf("page is missing %q", want)
			}
		}
	})

	t.Run("empty deployment gets the empty state", func(t *testing.T) {
		mux, _ := testMux(t)
		rec := getPage(mux, "/paths", nil)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "no paths yet") {
			t.Errorf("GET /paths = %d body missing empty state", rec.Code)
		}
	})
}

func TestPathPage(t *testing.T) {
	t.Run("renders the track in order", func(t *testing.T) {
		mux, fs := testMux(t)
		pathFixture(fs)
		rec := getPage(mux, "/paths/go-developer", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /paths/go-developer = %d", rec.Code)
		}
		body := rec.Body.String()
		for _, want := range []string{"Go Developer", "Why this order", "Go Basics", "Introduction to Concurrency", "~14h", "/courses/go-basics"} {
			if !strings.Contains(body, want) {
				t.Errorf("page is missing %q", want)
			}
		}
		// Track order: go-basics (step 01) must appear before concurrency.
		if strings.Index(body, "Go Basics") > strings.Index(body, "Introduction to Concurrency") {
			t.Error("courses rendered out of track order")
		}
	})

	t.Run("unknown path is 404", func(t *testing.T) {
		mux, fs := testMux(t)
		pathFixture(fs)
		if rec := getPage(mux, "/paths/nope", nil); rec.Code != http.StatusNotFound {
			t.Errorf("GET /paths/nope = %d, want 404", rec.Code)
		}
	})

	t.Run("logged-in progress marks completed courses", func(t *testing.T) {
		mux, fs := testMux(t)
		pathFixture(fs)
		fs.progress = []domain.VariantProgress{
			{CourseSlug: "go-basics", CourseTitle: "Go Basics", Language: "go", LessonsDone: 9, LessonsTotal: 9},
			{CourseSlug: "intro-to-concurrency", CourseTitle: "Introduction to Concurrency", Language: "go", LessonsDone: 1, LessonsTotal: 2},
		}
		session := loginAlice(t, mux)
		rec := getPage(mux, "/paths/go-developer", http.Header{"Cookie": {session.String()}})
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /paths/go-developer = %d", rec.Code)
		}
		body := rec.Body.String()
		for _, want := range []string{"1/2 courses complete", "9/9 lessons complete", "1/2 lessons complete"} {
			if !strings.Contains(body, want) {
				t.Errorf("page is missing %q", want)
			}
		}
	})

	t.Run("anonymous visitors get a signup nudge instead of progress", func(t *testing.T) {
		mux, fs := testMux(t)
		pathFixture(fs)
		rec := getPage(mux, "/paths/go-developer", nil)
		if !strings.Contains(rec.Body.String(), "to track your progress") {
			t.Error("page is missing the signup nudge")
		}
		if strings.Contains(rec.Body.String(), "courses complete") {
			t.Error("anonymous page shows a progress readout")
		}
	})
}

func TestCompletedCourses(t *testing.T) {
	courses := []domain.CourseSummary{{Slug: "a"}, {Slug: "b"}, {Slug: "c"}}
	progress := map[string]domain.VariantProgress{
		"a": {LessonsDone: 3, LessonsTotal: 3}, // done
		"b": {LessonsDone: 1, LessonsTotal: 3}, // started
		// c: never touched
	}
	if got := completedCourses(courses, progress); got != 1 {
		t.Errorf("completedCourses = %d, want 1", got)
	}
	if got := completedCourses(courses, map[string]domain.VariantProgress{}); got != 0 {
		t.Errorf("completedCourses with no progress = %d, want 0", got)
	}
	// A zero-lesson variant must not count as complete.
	if got := completedCourses([]domain.CourseSummary{{Slug: "z"}},
		map[string]domain.VariantProgress{"z": {}}); got != 0 {
		t.Errorf("empty variant counted as complete")
	}
}

// --- path proposal flow (the editor behind /paths/{slug}/edit) ---

const webPathDoc = `---
path: go-developer
title: Go Developer
description: From zero to production Go.
courses:
  - intro-to-concurrency
---

Start with the basics.
`

func TestEditPathPage(t *testing.T) {
	t.Run("prefills the stored document", func(t *testing.T) {
		mux, fs := testMux(t)
		pathFixture(fs)
		fs.path.SourceMD = webPathDoc
		fs.pathVersion = 1
		alice := loginAs(t, mux, "alice")

		rec := get(mux, "/paths/go-developer/edit", alice)
		if rec.Code != http.StatusOK {
			t.Fatalf("edit page = %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "path: go-developer") {
			t.Error("editor did not prefill the stored path document")
		}
	})

	t.Run("missing path 404s without new=1, seeds a template with it", func(t *testing.T) {
		mux, _ := testMux(t)
		alice := loginAs(t, mux, "alice")
		if rec := get(mux, "/paths/nope/edit", alice); rec.Code != http.StatusNotFound {
			t.Errorf("missing path edit = %d, want 404", rec.Code)
		}
		rec := get(mux, "/paths/nope/edit?new=1&title=Nope", alice)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "path: nope") {
			t.Errorf("new=1 edit = %d, want seeded template", rec.Code)
		}
	})
}

func TestProposePathAndAdminPublish(t *testing.T) {
	mux, fs := testMux(t)
	alice := loginAs(t, mux, "alice")

	rec := postForm(mux, "/paths/go-developer/edit",
		url.Values{"markdown": {webPathDoc}, "summary": {"new track"}}, alice)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("propose path = %d: %s", rec.Code, rec.Body)
	}
	if len(fs.proposals) != 1 {
		t.Fatalf("proposals = %d, want 1", len(fs.proposals))
	}
	p := fs.proposals[1]
	if p.Kind != domain.KindPath || p.CourseSlug != "go-developer" || p.Language != "" {
		t.Fatalf("proposal = %+v, want kind=path go-developer", p)
	}
	if p.Title != "Update path go-developer" {
		t.Errorf("default title = %q", p.Title)
	}

	// The detail page renders (diff against nothing — a new path).
	if rec := get(mux, "/proposals/1", nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "new path") {
		t.Errorf("detail page = %d, want 200 with the new-path badge", rec.Code)
	}

	// One admin approval publishes through the real parse + path publish.
	admin := loginAs(t, mux, "root")
	fs.promote("root", domain.RoleAdmin)
	rec = postForm(mux, "/proposals/1/reviews", url.Values{"verdict": {"approve"}, "revision": {"1"}}, admin)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("admin approve = %d: %s", rec.Code, rec.Body)
	}
	if fs.proposals[1].Status != domain.ProposalPublished {
		t.Fatalf("status = %q, want published", fs.proposals[1].Status)
	}
	if fs.path == nil || fs.path.Slug != "go-developer" || fs.path.SourceMD != webPathDoc {
		t.Errorf("published path = %+v, want the proposed document", fs.path)
	}
}

func TestProposePathValidation(t *testing.T) {
	mux, fs := testMux(t)
	alice := loginAs(t, mux, "alice")

	// Invalid document: editor re-renders with problems, nothing opened.
	rec := postForm(mux, "/paths/go-developer/edit",
		url.Values{"markdown": {"---\npath: go-developer\n---\nno title\n"}}, alice)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "problem") {
		t.Errorf("invalid doc = %d, want 200 re-render with problems", rec.Code)
	}
	// Frontmatter naming a different path: rejected.
	rec = postForm(mux, "/paths/other/edit", url.Values{"markdown": {webPathDoc}}, alice)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "frontmatter says") {
		t.Errorf("slug mismatch = %d, want 200 re-render with mismatch error", rec.Code)
	}
	if len(fs.proposals) != 0 {
		t.Errorf("proposals after failed submits = %d, want 0", len(fs.proposals))
	}
}
