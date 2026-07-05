package ingest

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCanonicalCoursesParse guards courses/*.md — the PR-reviewed content
// published via make publish — against agent-contract regressions.
func TestCanonicalCoursesParse(t *testing.T) {
	files, err := filepath.Glob(filepath.FromSlash("../../courses/*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no files matched ../../courses/*.md")
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Parse(src); err != nil {
				t.Fatalf("parse: %v", err)
			}
		})
	}
}
