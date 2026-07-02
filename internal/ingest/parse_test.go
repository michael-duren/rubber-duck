package ingest

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestParseSeedFixture(t *testing.T) {
	src, err := os.ReadFile("../../seed/intro-to-go.md")
	if err != nil {
		t.Fatal(err)
	}
	res, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if res.Course.Course != "intro-to-concurrency" || res.Course.Language != "go" {
		t.Errorf("frontmatter = %+v", res.Course)
	}
	if len(res.Course.Tags) != 2 || res.Course.Tags[0] != "backend" {
		t.Errorf("tags = %v", res.Course.Tags)
	}
	if len(res.Course.ExtendedReading) != 2 {
		t.Errorf("extended reading = %+v", res.Course.ExtendedReading)
	}

	if len(res.Lessons) != 2 {
		t.Fatalf("lessons = %d, want 2", len(res.Lessons))
	}
	l := res.Lessons[0]
	if l.Slug != "goroutines-basics" || l.Title != "Goroutines Basics" {
		t.Errorf("lesson 0 = %q %q", l.Slug, l.Title)
	}
	if !strings.Contains(l.ContentMD, "lightweight thread") {
		t.Errorf("lesson content missing prose: %q", l.ContentMD)
	}
	if !strings.Contains(l.ContentMD, "go func()") {
		t.Errorf("lesson content missing code example")
	}
	if strings.Contains(l.ContentMD, "Challenge") {
		t.Errorf("lesson content leaked challenge text")
	}

	if len(l.Challenges) != 1 {
		t.Fatalf("lesson 0 challenges = %d, want 1", len(l.Challenges))
	}
	c := l.Challenges[0]
	if c.Slug != "concurrent-sum" || c.Points != 10 {
		t.Errorf("challenge = %q points %d", c.Slug, c.Points)
	}
	if !strings.Contains(c.PromptMD, "splits the slice") {
		t.Errorf("prompt = %q", c.PromptMD)
	}
	if !strings.Contains(c.StarterCode, "func Sum(nums []int) int") {
		t.Errorf("starter = %q", c.StarterCode)
	}
	if !strings.Contains(c.TestCode, "func TestSum") {
		t.Errorf("tests = %q", c.TestCode)
	}

	if res.Final.Slug != "final" || res.Final.Points != 50 {
		t.Errorf("final = %+v", res.Final)
	}
	if !strings.Contains(res.Final.TestCode, "TestPipeline") {
		t.Errorf("final tests = %q", res.Final.TestCode)
	}
}

const validHeader = `---
course: c
title: T
language: go
description: d
---

`

const validLessonAndFinal = `
# Lesson: One {#one}

Body.

## Challenge: A {#a points=5}

Prompt.

### Starter

` + "```go\ncode\n```" + `

### Tests

` + "```go\ntests\n```" + `

# Final Challenge: F {#fin points=9}

### Starter

` + "```go\ns\n```" + `

### Tests

` + "```go\nt\n```" + `
`

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name    string
		doc     string
		wantMsg string // substring of some problem message
	}{
		{
			"missing frontmatter",
			"# Lesson: X {#x}\n" + validLessonAndFinal,
			"missing YAML frontmatter",
		},
		{
			"missing required field",
			"---\ncourse: c\ntitle: T\nlanguage: go\n---\n" + validLessonAndFinal,
			`missing required field "description"`,
		},
		{
			"no lessons",
			validHeader + "# Final Challenge: F {#fin points=9}\n\n### Starter\n\n```go\ns\n```\n\n### Tests\n\n```go\nt\n```\n",
			"no lessons",
		},
		{
			"no final challenge",
			validHeader + "# Lesson: One {#one}\n\nBody.\n",
			"no '# Final Challenge:'",
		},
		{
			"missing tests block",
			validHeader + `# Lesson: One {#one}

## Challenge: A {#a points=5}

### Starter

` + "```go\ncode\n```" + `

# Final Challenge: F {#fin points=9}

### Starter

` + "```go\ns\n```" + `

### Tests

` + "```go\nt\n```" + `
`,
			`challenge "a": missing '### Tests' block`,
		},
		{
			"missing points",
			validHeader + strings.Replace(validLessonAndFinal, "{#a points=5}", "{#a}", 1),
			"positive points=N",
		},
		{
			"missing slug",
			validHeader + strings.Replace(validLessonAndFinal, "{#one}", "", 1),
			"missing an {#slug}",
		},
		{
			"duplicate slug",
			validHeader + strings.Replace(validLessonAndFinal, "{#fin points=9}", "{#a points=9}", 1),
			`duplicate slug "a"`,
		},
		{
			"challenge outside lesson",
			validHeader + `## Challenge: A {#a points=5}

### Starter

` + "```go\nc\n```" + `

### Tests

` + "```go\nt\n```" + `
` + validLessonAndFinal,
			"challenge outside a lesson",
		},
		{
			"two final challenges",
			validHeader + validLessonAndFinal + `
# Final Challenge: G {#fin2 points=3}

### Starter

` + "```go\ns\n```" + `

### Tests

` + "```go\nt\n```" + `
`,
			"more than one final challenge",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse([]byte(c.doc))
			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("err = %v, want ValidationError", err)
			}
			for _, p := range verr.Problems {
				if strings.Contains(p.Message, c.wantMsg) {
					if p.Line <= 0 {
						t.Errorf("problem has no line number: %+v", p)
					}
					return
				}
			}
			t.Errorf("no problem contains %q; got %v", c.wantMsg, verr.Problems)
		})
	}
}

func TestParseErrorLineNumbers(t *testing.T) {
	doc := validHeader + `# Lesson: One {#one}

## Challenge: A {#a points=5}

### Starter

` + "```go\ncode\n```" + `

# Final Challenge: F {#fin points=9}

### Starter

` + "```go\ns\n```" + `

### Tests

` + "```go\nt\n```" + `
`
	_, err := Parse([]byte(doc))
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("err = %v", err)
	}
	// Frontmatter spans lines 1-6, blank line 7, lesson heading line 8,
	// blank line 9, so the challenge heading sits on line 10.
	for _, p := range verr.Problems {
		if strings.Contains(p.Message, "missing '### Tests'") {
			if p.Line != 10 {
				t.Errorf("line = %d, want 10 (problems: %v)", p.Line, verr.Problems)
			}
			return
		}
	}
	t.Fatal("expected missing-tests problem")
}
