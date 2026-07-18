package ingest

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCanonicalCoursesParse guards courses/*.md — the mirror of what's
// published on the server (synced by course-sync.yml, importable via
// `make seed` / `make import-courses-prod`) — against contract regressions.
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
