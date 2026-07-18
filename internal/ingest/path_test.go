package ingest

import (
	"errors"
	"strings"
	"testing"
)

const validPathDoc = `---
path: go-developer
title: Go Developer
description: From zero to production Go.
courses:
  - go-basics
  - intro-to-concurrency
---

## Why this order

Basics first, then concurrency.
`

func TestParsePath(t *testing.T) {
	res, err := ParsePath([]byte(validPathDoc))
	if err != nil {
		t.Fatalf("ParsePath: %v", err)
	}
	if res.Path.Path != "go-developer" || res.Path.Title != "Go Developer" {
		t.Errorf("frontmatter = %+v", res.Path)
	}
	if len(res.Path.Courses) != 2 || res.Path.Courses[0] != "go-basics" {
		t.Errorf("courses = %v", res.Path.Courses)
	}
	if !strings.HasPrefix(res.OverviewMD, "## Why this order") {
		t.Errorf("overview = %q", res.OverviewMD)
	}
}

func TestParsePathProblems(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		want string // substring of one reported problem
	}{
		{"no frontmatter", "just some text\n", "missing YAML frontmatter"},
		{"missing fields", "---\npath: x\ncourses: [a]\n---\n", `missing required field "title"`},
		{"empty courses", "---\npath: x\ntitle: X\ndescription: d\n---\n", `"courses" list`},
		{"duplicate course", "---\npath: x\ntitle: X\ndescription: d\ncourses: [a, a]\n---\n", `duplicate course slug "a"`},
		{"empty course slug", "---\npath: x\ntitle: X\ndescription: d\ncourses: [\"\"]\n---\n", "empty slug"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParsePath([]byte(c.doc))
			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("err = %v, want *ValidationError", err)
			}
			for _, p := range verr.Problems {
				if strings.Contains(p.Message, c.want) {
					return
				}
			}
			t.Errorf("no problem mentions %q in %v", c.want, verr.Problems)
		})
	}
}

func TestIsPathDocument(t *testing.T) {
	if !IsPathDocument([]byte(validPathDoc)) {
		t.Error("path document not recognized")
	}
	course := "---\ncourse: intro\ntitle: T\nlanguage: go\ndescription: d\n---\n"
	if IsPathDocument([]byte(course)) {
		t.Error("course document misrecognized as path")
	}
	if IsPathDocument([]byte("no frontmatter at all")) {
		t.Error("plain text misrecognized as path")
	}
}

func TestPathToDomain(t *testing.T) {
	res, err := ParsePath([]byte(validPathDoc))
	if err != nil {
		t.Fatal(err)
	}
	p, err := PathToDomain(res, []byte(validPathDoc))
	if err != nil {
		t.Fatal(err)
	}
	if p.Slug != "go-developer" || len(p.CourseSlugs) != 2 {
		t.Errorf("path = %+v", p)
	}
	if !strings.Contains(p.OverviewHTML, "<h2") || !strings.Contains(p.DescriptionHTML, "<p>") {
		t.Errorf("HTML not rendered: overview=%q description=%q", p.OverviewHTML, p.DescriptionHTML)
	}
	if p.SourceMD != validPathDoc {
		t.Error("source markdown not kept verbatim")
	}
}
