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

// --- endpoint tests (reads only: path writes go through `duckserver
// seed`, so there is no PUT/DELETE surface here) ---

const pathDoc = `---
path: go-developer
title: Go Developer
description: From zero to production Go.
courses:
  - intro-to-concurrency
---

Start with the basics, then build up.
`

func TestGetPaths(t *testing.T) {
	mux, fs := testAPI(t)
	fs.courses["intro-to-concurrency"] = domain.Course{Slug: "intro-to-concurrency", Title: "Intro to Concurrency"}
	fs.paths["go-developer"] = domain.LearningPath{
		Slug: "go-developer", Title: "Go Developer",
		SourceMD:    pathDoc,
		CourseSlugs: []string{"intro-to-concurrency"},
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

	if rec := doJSON(mux, "GET", "/api/v1/paths/nope", nil); rec.Code != http.StatusNotFound {
		t.Errorf("get missing = %d, want 404", rec.Code)
	}
}
