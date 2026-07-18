package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/michael-duren/rubber-duck/internal/domain"
)

// --- fakeStore's learning-path slice (fields in courses_test.go) ---

func (f *fakeStore) UpsertPath(_ context.Context, p domain.LearningPath) (bool, error) {
	var missing []string
	for _, slug := range p.CourseSlugs {
		if _, ok := f.courses[slug]; !ok {
			missing = append(missing, slug)
		}
	}
	if len(missing) > 0 {
		return false, &domain.UnknownCoursesError{Slugs: missing}
	}
	_, existed := f.paths[p.Slug]
	f.paths[p.Slug] = p
	return !existed, nil
}

func (f *fakeStore) ListPaths(context.Context) ([]domain.PathSummary, error) {
	var out []domain.PathSummary
	for _, p := range f.paths {
		out = append(out, domain.PathSummary{
			Slug: p.Slug, Title: p.Title,
			DescriptionHTML: p.DescriptionHTML, CourseCount: len(p.CourseSlugs),
		})
	}
	return out, nil
}

func (f *fakeStore) PathBySlug(_ context.Context, slug string) (domain.LearningPath, []domain.CourseSummary, error) {
	p, ok := f.paths[slug]
	if !ok {
		return domain.LearningPath{}, nil, domain.ErrNotFound
	}
	var courses []domain.CourseSummary
	for _, cs := range p.CourseSlugs {
		c := f.courses[cs]
		courses = append(courses, domain.CourseSummary{Slug: c.Slug, Title: c.Title})
	}
	return p, courses, nil
}

func (f *fakeStore) DeletePath(_ context.Context, slug string) error {
	if _, ok := f.paths[slug]; !ok {
		return domain.ErrNotFound
	}
	delete(f.paths, slug)
	return nil
}

// --- endpoint tests ---

const pathDoc = `---
path: go-developer
title: Go Developer
description: From zero to production Go.
courses:
  - intro-to-concurrency
---

Start with the basics, then build up.
`

// seedCourse publishes the seed course so path documents have a member
// course to reference.
func seedCourse(t *testing.T, mux *http.ServeMux) {
	t.Helper()
	rec := doJSON(mux, "PUT", "/api/v1/courses/intro-to-concurrency/variants/go",
		map[string]string{"markdown": seedDoc(t)})
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed course = %d body %s", rec.Code, rec.Body)
	}
}

func TestPutPath(t *testing.T) {
	t.Run("create then update", func(t *testing.T) {
		mux, _ := testAPI(t)
		seedCourse(t, mux)

		rec := doJSON(mux, "PUT", "/api/v1/paths/go-developer", map[string]string{"markdown": pathDoc})
		if rec.Code != http.StatusCreated {
			t.Fatalf("first put = %d body %s", rec.Code, rec.Body)
		}
		var resp struct {
			Path    string   `json:"path"`
			Courses []string `json:"courses"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Path != "go-developer" || len(resp.Courses) != 1 {
			t.Errorf("resp = %+v", resp)
		}

		rec = doJSON(mux, "PUT", "/api/v1/paths/go-developer", map[string]string{"markdown": pathDoc})
		if rec.Code != http.StatusOK {
			t.Errorf("second put = %d, want 200", rec.Code)
		}
	})

	t.Run("slug mismatch is 409", func(t *testing.T) {
		mux, _ := testAPI(t)
		seedCourse(t, mux)
		rec := doJSON(mux, "PUT", "/api/v1/paths/other-name", map[string]string{"markdown": pathDoc})
		if rec.Code != http.StatusConflict {
			t.Errorf("put = %d body %s, want 409", rec.Code, rec.Body)
		}
	})

	t.Run("invalid document is 422 with line problems", func(t *testing.T) {
		mux, _ := testAPI(t)
		rec := doJSON(mux, "PUT", "/api/v1/paths/go-developer",
			map[string]string{"markdown": "---\npath: go-developer\n---\nno title, no courses\n"})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("put = %d body %s, want 422", rec.Code, rec.Body)
		}
		if !strings.Contains(rec.Body.String(), "invalid_path_markdown") {
			t.Errorf("body = %s", rec.Body)
		}
	})

	t.Run("unknown course slug is 422", func(t *testing.T) {
		mux, _ := testAPI(t)
		// No course seeded: intro-to-concurrency doesn't exist.
		rec := doJSON(mux, "PUT", "/api/v1/paths/go-developer", map[string]string{"markdown": pathDoc})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("put = %d body %s, want 422", rec.Code, rec.Body)
		}
		if !strings.Contains(rec.Body.String(), "unknown_course_slugs") ||
			!strings.Contains(rec.Body.String(), "intro-to-concurrency") {
			t.Errorf("body = %s", rec.Body)
		}
	})
}

func TestGetDeletePath(t *testing.T) {
	mux, _ := testAPI(t)
	seedCourse(t, mux)
	if rec := doJSON(mux, "PUT", "/api/v1/paths/go-developer", map[string]string{"markdown": pathDoc}); rec.Code != http.StatusCreated {
		t.Fatalf("put = %d body %s", rec.Code, rec.Body)
	}

	rec := doJSON(mux, "GET", "/api/v1/paths/go-developer", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d body %s", rec.Code, rec.Body)
	}
	var got struct {
		Markdown string   `json:"markdown"`
		Courses  []string `json:"courses"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Markdown != pathDoc {
		t.Errorf("markdown did not round-trip:\n%s", got.Markdown)
	}
	if len(got.Courses) != 1 || got.Courses[0] != "intro-to-concurrency" {
		t.Errorf("courses = %v", got.Courses)
	}

	if rec := doJSON(mux, "GET", "/api/v1/paths", nil); rec.Code != http.StatusOK ||
		!strings.Contains(rec.Body.String(), "go-developer") {
		t.Errorf("list = %d body %s", rec.Code, rec.Body)
	}

	if rec := doJSON(mux, "DELETE", "/api/v1/paths/go-developer", nil); rec.Code != http.StatusNoContent {
		t.Errorf("delete = %d", rec.Code)
	}
	if rec := doJSON(mux, "GET", "/api/v1/paths/go-developer", nil); rec.Code != http.StatusNotFound {
		t.Errorf("get after delete = %d", rec.Code)
	}
}
