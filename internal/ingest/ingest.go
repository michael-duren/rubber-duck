// Package ingest parses agent-submitted course markdown into domain types.
//
// The document convention (one file per course variant):
//
//	--- yaml frontmatter: course, title, language, description required ---
//	# Lesson: Title {#slug}
//	  lesson content…
//	## Challenge: Title {#slug points=N}
//	  prompt…
//	### Starter
//	  ```lang … ```
//	### Tests
//	  ```lang … ```
//	# Final Challenge: Title {#slug points=N}   (exactly one)
package ingest

import (
	"fmt"
	"strings"
)

// Result is a fully parsed and validated course variant.
type Result struct {
	Course  Frontmatter
	Lessons []ParsedLesson
	Final   ParsedChallenge
}

type Frontmatter struct {
	Course          string    `yaml:"course"`
	Title           string    `yaml:"title"`
	Language        string    `yaml:"language"`
	Description     string    `yaml:"description"`
	DurationHours   float64   `yaml:"duration_hours"`
	Tags            []string  `yaml:"tags"`
	ExtendedReading []Reading `yaml:"extended_reading"`
}

type Reading struct {
	Title string `yaml:"title"`
	URL   string `yaml:"url"`
}

type ParsedLesson struct {
	Slug       string
	Title      string
	ContentMD  string
	Challenges []ParsedChallenge
}

type ParsedChallenge struct {
	Slug        string
	Title       string
	PromptMD    string
	StarterCode string
	TestCode    string
	Points      int
	Line        int // source line of the heading, for error reporting
}

// Problem is a single validation failure tied to a source line.
type Problem struct {
	Line    int
	Message string
}

// ValidationError aggregates every problem found so agents can fix a
// document in one round trip.
type ValidationError struct {
	Problems []Problem
}

func (e *ValidationError) Error() string {
	msgs := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		msgs[i] = fmt.Sprintf("line %d: %s", p.Line, p.Message)
	}
	return fmt.Sprintf("%d problems: %s", len(e.Problems), strings.Join(msgs, "; "))
}
