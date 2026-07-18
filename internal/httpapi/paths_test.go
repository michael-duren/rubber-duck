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

func (f *fakeStore) ListPathSources(context.Context) ([]domain.PathExport, error) {
	var out []domain.PathExport
	for _, p := range f.paths {
		out = append(out, domain.PathExport{Slug: p.Slug, Version: 1, SourceMD: p.SourceMD})
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

func TestCreatePathProposal(t *testing.T) {
	t.Run("path document creates a kind=path proposal", func(t *testing.T) {
		mux, fs := testAPI(t)
		fs.addUser("gc_u_alice", domain.User{ID: 42, Username: "alice"})
		fs.courses["intro-to-concurrency"] = domain.Course{Slug: "intro-to-concurrency", Title: "Intro"}

		rec := doJSONAs(mux, "POST", "/api/v1/proposals", "gc_u_alice",
			map[string]string{"markdown": pathDoc, "summary": "new track"})
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d body %s", rec.Code, rec.Body)
		}
		var got map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if got["kind"] != "path" || got["path"] != "go-developer" ||
			got["title"] != "Update path go-developer" || got["status"] != "open" {
			t.Errorf("response = %v", got)
		}
		if _, hasCourse := got["course"]; hasCourse {
			t.Errorf("path proposal response leaked a course field: %v", got)
		}
	})

	t.Run("course/language body fields on a path document are 409", func(t *testing.T) {
		mux, fs := testAPI(t)
		fs.addUser("gc_u_alice", domain.User{ID: 42, Username: "alice"})
		rec := doJSONAs(mux, "POST", "/api/v1/proposals", "gc_u_alice",
			map[string]string{"markdown": pathDoc, "course": "intro-to-concurrency", "language": "go"})
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "slug_mismatch") {
			t.Errorf("status = %d body %s, want 409 slug_mismatch", rec.Code, rec.Body)
		}
	})

	t.Run("path body field mismatch is 409", func(t *testing.T) {
		mux, fs := testAPI(t)
		fs.addUser("gc_u_alice", domain.User{ID: 42, Username: "alice"})
		rec := doJSONAs(mux, "POST", "/api/v1/proposals", "gc_u_alice",
			map[string]string{"markdown": pathDoc, "path": "other-track"})
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "slug_mismatch") {
			t.Errorf("status = %d body %s, want 409 slug_mismatch", rec.Code, rec.Body)
		}
	})

	t.Run("invalid path document is 422 with line problems", func(t *testing.T) {
		mux, fs := testAPI(t)
		fs.addUser("gc_u_alice", domain.User{ID: 42, Username: "alice"})
		rec := doJSONAs(mux, "POST", "/api/v1/proposals", "gc_u_alice",
			map[string]string{"markdown": "---\npath: go-developer\n---\nno title, no courses\n"})
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "invalid_path_markdown") {
			t.Errorf("status = %d body %s, want 422 invalid_path_markdown", rec.Code, rec.Body)
		}
	})

	t.Run("update cannot retarget a course proposal to a path", func(t *testing.T) {
		mux, fs := testAPI(t)
		fs.addUser("gc_u_alice", domain.User{ID: 42, Username: "alice"})
		course := seedDoc(t)
		fs.seedVariant(t, course)
		if rec := doJSONAs(mux, "POST", "/api/v1/proposals", "gc_u_alice", map[string]string{"markdown": course}); rec.Code != http.StatusCreated {
			t.Fatalf("create course proposal = %d body %s", rec.Code, rec.Body)
		}
		rec := doJSONAs(mux, "PUT", "/api/v1/proposals/1", "gc_u_alice", map[string]string{"markdown": pathDoc})
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "variant_mismatch") {
			t.Errorf("retarget = %d body %s, want 409 variant_mismatch", rec.Code, rec.Body)
		}
	})
}

func TestExportIncludesPaths(t *testing.T) {
	mux, fs := testAPI(t)
	fs.paths["go-developer"] = domain.LearningPath{Slug: "go-developer", SourceMD: pathDoc}

	rec := doJSON(mux, "GET", "/api/v1/export", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("export = %d", rec.Code)
	}
	var got struct {
		Paths []struct {
			Path     string `json:"path"`
			Markdown string `json:"markdown"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Paths) != 1 || got.Paths[0].Path != "go-developer" || got.Paths[0].Markdown != pathDoc {
		t.Errorf("export paths = %+v", got.Paths)
	}
}
