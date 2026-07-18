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

// TestParseSectionBoundaries pins down where a section's markdown ends: at
// the start of the next boundary heading's line. A regression here leaks the
// next heading's "#"/"##"/"###" marker characters into the stored content.
func TestParseSectionBoundaries(t *testing.T) {
	res, err := Parse([]byte(validHeader + validLessonAndFinal))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := res.Lessons[0].ContentMD; got != "Body." {
		t.Errorf("lesson content = %q, want %q", got, "Body.")
	}
	if got := res.Lessons[0].Challenges[0].PromptMD; got != "Prompt." {
		t.Errorf("challenge prompt = %q, want %q", got, "Prompt.")
	}
	if got := res.Final.PromptMD; got != "" {
		t.Errorf("final prompt = %q, want empty", got)
	}
}

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
			"course slug with path separators",
			"---\ncourse: ../evil\ntitle: T\nlanguage: go\ndescription: D\n---\n" + validLessonAndFinal,
			`field "course" must be lowercase letters, numbers, and hyphens`,
		},
		{
			"language not kebab-case",
			"---\ncourse: c\ntitle: T\nlanguage: C++\ndescription: D\n---\n" + validLessonAndFinal,
			`field "language" must be lowercase letters, numbers, and hyphens`,
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

// A challenge slug shaped like a `duck pull` ordering prefix would strip
// back to the wrong slug on the learner's machine, so ingest rejects it —
// except a final challenge named "final-…", whose pulled directory gets a
// second "final-" prepended and still strips back correctly.
func TestReservedSlugShapes(t *testing.T) {
	lessonChallenge := func(slug string) string {
		return validHeader + strings.Replace(validLessonAndFinal, "{#a ", "{#"+slug+" ", 1)
	}
	finalChallenge := func(slug string) string {
		return validHeader + strings.Replace(validLessonAndFinal, "{#fin ", "{#"+slug+" ", 1)
	}

	cases := []struct {
		name   string
		doc    string
		reject bool
	}{
		{"lesson challenge, digit prefix", lessonChallenge("64-bit-ints"), true},
		{"lesson challenge, digit+letter prefix", lessonChallenge("05a-heap"), true},
		{"lesson challenge, final- prefix", lessonChallenge("final-lap"), true},
		{"final, digit prefix", finalChallenge("03-boss"), true},
		{"lesson challenge, single digit is a slug", lessonChallenge("3-way-partition"), false},
		{"final may be named final-", finalChallenge("final-duckos"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse([]byte(c.doc))
			var verr *ValidationError
			if errors.As(err, &verr) {
				for _, p := range verr.Problems {
					if strings.Contains(p.Message, "ordering prefix") {
						if !c.reject {
							t.Errorf("slug rejected: %v", p)
						} else if p.Line <= 0 {
							t.Errorf("problem has no line number: %+v", p)
						}
						return
					}
				}
			}
			if c.reject {
				t.Error("slug accepted, want an ordering-prefix problem")
			}
		})
	}
}
