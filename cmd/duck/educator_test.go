package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validCourseMD is a minimal document that internal/ingest.Parse accepts,
// used as the "server has this markdown" fixture for pull/push tests.
const validCourseMD = `---
course: intro-to-concurrency
title: Introduction to Concurrency
language: go
description: Learn goroutines and channels.
duration_hours: 1
---

# Lesson: Goroutines Basics {#goroutines-basics}

A goroutine is a lightweight thread.

## Challenge: Start One {#start-one points=10}

Write a function that starts a goroutine.

### Starter

` + "```go\nfunc Start() {}\n```" + `

### Tests

` + "```go\nfunc TestStart(t *testing.T) {}\n```" + `

# Final Challenge: Put It Together {#final points=20}

Combine what you've learned.

### Starter

` + "```go\nfunc Final() {}\n```" + `

### Tests

` + "```go\nfunc TestFinal(t *testing.T) {}\n```" + `
`

func TestLintSource(t *testing.T) {
	if probs := lintSource([]byte(validCourseMD)); len(probs) != 0 {
		t.Errorf("valid doc: got %d problems, want 0: %+v", len(probs), probs)
	}

	probs := lintSource([]byte("not a course document at all"))
	if len(probs) == 0 {
		t.Fatal("invalid doc: got 0 problems, want at least 1")
	}
	if probs[0].Line != 1 {
		t.Errorf("first problem line = %d, want 1", probs[0].Line)
	}
	if !strings.Contains(probs[0].Message, "frontmatter") {
		t.Errorf("first problem message = %q, want mention of frontmatter", probs[0].Message)
	}
}

func TestEducatorPull(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("DUCK_TOKEN", "gc_u_testtoken")

	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/api/v1/courses/intro-to-concurrency/variants/go" {
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer gc_u_testtoken" {
				t.Errorf("Authorization = %q", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"markdown": validCourseMD, "version": 3})
		}))
		defer srv.Close()

		if err := educatorPullCmd([]string{"intro-to-concurrency/go", "--base", srv.URL}); err != nil {
			t.Fatalf("educatorPullCmd: %v", err)
		}

		mdPath := "intro-to-concurrency-go.md"
		got, err := os.ReadFile(mdPath)
		if err != nil {
			t.Fatalf("read %s: %v", mdPath, err)
		}
		if string(got) != validCourseMD {
			t.Errorf("wrote %q, want the server's markdown", got)
		}

		meta, err := readEducatorMeta(mdPath)
		if err != nil {
			t.Fatalf("readEducatorMeta: %v", err)
		}
		want := educatorMeta{BaseURL: srv.URL, Course: "intro-to-concurrency", Language: "go", Version: 3}
		if meta != want {
			t.Errorf("sidecar = %+v, want %+v", meta, want)
		}
	})

	t.Run("unauthorized", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "unauthorized", "message": "nope"}})
		}))
		defer srv.Close()

		err := educatorPullCmd([]string{"nope/go", "--base", srv.URL})
		if err == nil || !strings.Contains(err.Error(), "unauthorized") {
			t.Fatalf("educatorPullCmd err = %v, want unauthorized error", err)
		}
	})

	t.Run("bad usage", func(t *testing.T) {
		if err := educatorPullCmd(nil); err == nil {
			t.Fatal("want error for missing course/language arg")
		}
		if err := educatorPullCmd([]string{"no-slash"}); err == nil {
			t.Fatal("want error for arg missing a slash")
		}
	})

	t.Run("refuses to overwrite differing local file without --force", func(t *testing.T) {
		t.Chdir(t.TempDir())
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"markdown": validCourseMD, "version": 5})
		}))
		defer srv.Close()

		mdPath := "intro-to-concurrency-go.md"
		local := "# my unpushed local edits"
		if err := os.WriteFile(mdPath, []byte(local), 0o644); err != nil {
			t.Fatal(err)
		}

		err := educatorPullCmd([]string{"intro-to-concurrency/go", "--base", srv.URL})
		if err == nil || !strings.Contains(err.Error(), "refusing to overwrite") {
			t.Fatalf("educatorPullCmd err = %v, want a refusing-to-overwrite error", err)
		}
		if got, _ := os.ReadFile(mdPath); string(got) != local {
			t.Fatalf("local file was modified by a refused pull: %q", got)
		}

		if err := educatorPullCmd([]string{"intro-to-concurrency/go", "--base", srv.URL, "--force"}); err != nil {
			t.Fatalf("educatorPullCmd --force: %v", err)
		}
		if got, _ := os.ReadFile(mdPath); string(got) != validCourseMD {
			t.Errorf("--force pull did not replace the local file")
		}
	})

	t.Run("identical local file pulls without --force", func(t *testing.T) {
		t.Chdir(t.TempDir())
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"markdown": validCourseMD, "version": 5})
		}))
		defer srv.Close()

		if err := os.WriteFile("intro-to-concurrency-go.md", []byte(validCourseMD), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := educatorPullCmd([]string{"intro-to-concurrency/go", "--base", srv.URL}); err != nil {
			t.Fatalf("educatorPullCmd over identical file: %v", err)
		}
	})

	t.Run("response missing version errors instead of zero-defaulting", func(t *testing.T) {
		t.Chdir(t.TempDir())
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// An older server whose GET response is markdown-only.
			_ = json.NewEncoder(w).Encode(map[string]any{"markdown": validCourseMD})
		}))
		defer srv.Close()

		err := educatorPullCmd([]string{"intro-to-concurrency/go", "--base", srv.URL})
		if err == nil || !strings.Contains(err.Error(), "missing markdown or version") {
			t.Fatalf("educatorPullCmd err = %v, want a missing-version error", err)
		}
		if _, statErr := os.Stat("intro-to-concurrency-go.md"); statErr == nil {
			t.Error("pull wrote a file despite the invalid response")
		}
	})

	t.Run("non-200 error includes the status", func(t *testing.T) {
		t.Chdir(t.TempDir())
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "not_found", "message": "no such course variant"}})
		}))
		defer srv.Close()

		err := educatorPullCmd([]string{"nope/go", "--base", srv.URL})
		if err == nil || !strings.Contains(err.Error(), "404") {
			t.Fatalf("educatorPullCmd err = %v, want the 404 status surfaced", err)
		}
	})
}

// pushFixture writes a markdown file and its sidecar directly (bypassing
// pull) so push subtests can start from a known version without a network
// round trip.
func pushFixture(t *testing.T, dir string, meta educatorMeta, markdown string) string {
	t.Helper()
	mdPath := filepath.Join(dir, meta.Course+"-"+meta.Language+".md")
	if err := os.WriteFile(mdPath, []byte(markdown), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeEducatorMeta(mdPath, meta); err != nil {
		t.Fatal(err)
	}
	return mdPath
}

func TestEducatorPush(t *testing.T) {
	t.Setenv("DUCK_TOKEN", "gc_u_testtoken")

	t.Run("success updates sidecar version", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		var gotBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPut || r.URL.Path != "/api/v1/courses/intro-to-concurrency/variants/go" {
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer gc_u_testtoken" {
				t.Errorf("Authorization = %q", got)
			}
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"course": "intro-to-concurrency", "language": "go",
				"version": 4, "lessons": 1, "challenges": 2, "total_points": 30,
			})
		}))
		defer srv.Close()

		pushFixture(t, dir, educatorMeta{BaseURL: srv.URL, Course: "intro-to-concurrency", Language: "go", Version: 3}, validCourseMD)

		if err := educatorPushCmd(nil); err != nil {
			t.Fatalf("educatorPushCmd: %v", err)
		}
		if ev, _ := gotBody["expected_version"].(float64); int(ev) != 3 {
			t.Errorf("server received expected_version = %v, want 3", gotBody["expected_version"])
		}

		meta, err := readEducatorMeta(filepath.Join(dir, "intro-to-concurrency-go.md"))
		if err != nil {
			t.Fatalf("readEducatorMeta: %v", err)
		}
		if meta.Version != 4 {
			t.Errorf("sidecar version = %d, want 4 (updated from response)", meta.Version)
		}
	})

	t.Run("version conflict does not update sidecar", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
				"code": "version_conflict", "message": "stale",
			}})
		}))
		defer srv.Close()

		mdPath := pushFixture(t, dir, educatorMeta{BaseURL: srv.URL, Course: "intro-to-concurrency", Language: "go", Version: 3}, validCourseMD)

		err := educatorPushCmd(nil)
		if err == nil || !strings.Contains(err.Error(), "duck educator pull") {
			t.Fatalf("educatorPushCmd err = %v, want a re-pull message", err)
		}

		meta, err := readEducatorMeta(mdPath)
		if err != nil {
			t.Fatalf("readEducatorMeta: %v", err)
		}
		if meta.Version != 3 {
			t.Errorf("sidecar version = %d, want unchanged 3 after conflict", meta.Version)
		}
	})

	t.Run("server-side invalid markdown prints details", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
				"code": "invalid_course_markdown", "message": "1 problems found",
				"details": []map[string]any{{"line": 12, "message": "missing starter code"}},
			}})
		}))
		defer srv.Close()

		// The server round trip only happens if the local lint passes, so
		// this exercises the 422 path specifically: use markdown that
		// passes lintSource's local check but which the server (in this
		// fake) rejects anyway.
		pushFixture(t, dir, educatorMeta{BaseURL: srv.URL, Course: "intro-to-concurrency", Language: "go", Version: 3}, validCourseMD)

		err := educatorPushCmd(nil)
		if err == nil || !strings.Contains(err.Error(), "1 problem") {
			t.Fatalf("educatorPushCmd err = %v, want a problem-count error", err)
		}
	})

	t.Run("locally-invalid markdown never hits the network", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		called := false
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
		}))
		defer srv.Close()

		pushFixture(t, dir, educatorMeta{BaseURL: srv.URL, Course: "bad", Language: "go", Version: 1}, "not a course document")

		if err := educatorPushCmd(nil); err == nil {
			t.Fatal("want error for locally-invalid markdown")
		}
		if called {
			t.Error("server should not have been called when local lint fails")
		}
	})

	t.Run("no sidecar in directory errors clearly", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		if err := educatorPushCmd(nil); err == nil || !strings.Contains(err.Error(), "duck educator pull") {
			t.Fatalf("educatorPushCmd err = %v, want a pull-first message", err)
		}
	})

	t.Run("explicit file argument works from another directory", func(t *testing.T) {
		fixtureDir := t.TempDir()
		t.Chdir(t.TempDir()) // cwd deliberately holds neither file nor sidecar

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"course": "intro-to-concurrency", "language": "go",
				"version": 4, "lessons": 1, "challenges": 2, "total_points": 30,
			})
		}))
		defer srv.Close()

		mdPath := pushFixture(t, fixtureDir, educatorMeta{BaseURL: srv.URL, Course: "intro-to-concurrency", Language: "go", Version: 3}, validCourseMD)

		if err := educatorPushCmd([]string{mdPath}); err != nil {
			t.Fatalf("educatorPushCmd(explicit path): %v", err)
		}
		meta, err := readEducatorMeta(mdPath)
		if err != nil {
			t.Fatalf("readEducatorMeta: %v", err)
		}
		if meta.Version != 4 {
			t.Errorf("sidecar next to the explicit path has version %d, want 4", meta.Version)
		}
	})

	t.Run("unauthorized", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "unauthorized", "message": "nope"}})
		}))
		defer srv.Close()

		pushFixture(t, dir, educatorMeta{BaseURL: srv.URL, Course: "intro-to-concurrency", Language: "go", Version: 3}, validCourseMD)

		err := educatorPushCmd(nil)
		if err == nil || !strings.Contains(err.Error(), "unauthorized") {
			t.Fatalf("educatorPushCmd err = %v, want unauthorized error", err)
		}
	})

	t.Run("corrupt sidecar json errors", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		mdPath := filepath.Join(dir, "a-go.md")
		if err := os.WriteFile(mdPath, []byte(validCourseMD), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(mdPath+educatorMetaSuffix, []byte("{not json"), 0o644); err != nil {
			t.Fatal(err)
		}
		err := educatorPushCmd(nil)
		if err == nil || !strings.Contains(err.Error(), "parse") {
			t.Fatalf("educatorPushCmd err = %v, want a sidecar parse error", err)
		}
	})

	t.Run("sidecar missing fields errors actionably", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		mdPath := filepath.Join(dir, "a-go.md")
		if err := os.WriteFile(mdPath, []byte(validCourseMD), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(mdPath+educatorMetaSuffix, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		err := educatorPushCmd(nil)
		if err == nil || !strings.Contains(err.Error(), "re-run `duck educator pull`") {
			t.Fatalf("educatorPushCmd err = %v, want a corrupt-sidecar message", err)
		}
	})

	t.Run("unparseable success response says the push succeeded", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("not json at all"))
		}))
		defer srv.Close()

		mdPath := pushFixture(t, dir, educatorMeta{BaseURL: srv.URL, Course: "intro-to-concurrency", Language: "go", Version: 3}, validCourseMD)

		err := educatorPushCmd(nil)
		if err == nil || !strings.Contains(err.Error(), "push succeeded on the server") {
			t.Fatalf("educatorPushCmd err = %v, want a push-succeeded-but message", err)
		}
		// The sidecar is knowingly stale here; the message (asserted above)
		// is what points the user at pull to resync.
		meta, err := readEducatorMeta(mdPath)
		if err != nil {
			t.Fatalf("readEducatorMeta: %v", err)
		}
		if meta.Version != 3 {
			t.Errorf("sidecar version = %d, want unchanged 3", meta.Version)
		}
	})

	t.Run("non-version_conflict 409 surfaces the server message", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
				"code": "slug_mismatch", "message": "URL names a/go but frontmatter says b/go",
			}})
		}))
		defer srv.Close()

		pushFixture(t, dir, educatorMeta{BaseURL: srv.URL, Course: "intro-to-concurrency", Language: "go", Version: 3}, validCourseMD)

		err := educatorPushCmd(nil)
		if err == nil || !strings.Contains(err.Error(), "frontmatter says") {
			t.Fatalf("educatorPushCmd err = %v, want the server's slug_mismatch message", err)
		}
	})
}

func TestEducatorLint(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	t.Run("valid file", func(t *testing.T) {
		path := filepath.Join(dir, "ok.md")
		if err := os.WriteFile(path, []byte(validCourseMD), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := educatorLintCmd([]string{path}); err != nil {
			t.Errorf("educatorLintCmd(valid): %v", err)
		}
	})

	t.Run("invalid file", func(t *testing.T) {
		path := filepath.Join(dir, "bad.md")
		if err := os.WriteFile(path, []byte("nope"), 0o644); err != nil {
			t.Fatal(err)
		}
		err := educatorLintCmd([]string{path})
		if err == nil || !strings.Contains(err.Error(), "problem") {
			t.Errorf("educatorLintCmd(invalid) = %v, want a problem-count error", err)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		if err := educatorLintCmd([]string{filepath.Join(dir, "missing.md")}); err == nil {
			t.Error("want error for missing file")
		}
	})
}

func TestFindEducatorFile(t *testing.T) {
	dir := t.TempDir()

	if _, err := findEducatorFile(dir); err == nil {
		t.Error("empty dir: want error, got nil")
	}

	mdPath := filepath.Join(dir, "a-go.md")
	if err := writeEducatorMeta(mdPath, educatorMeta{Course: "a", Language: "go", Version: 1}); err != nil {
		t.Fatal(err)
	}
	got, err := findEducatorFile(dir)
	if err != nil {
		t.Fatalf("findEducatorFile: %v", err)
	}
	if got != mdPath {
		t.Errorf("findEducatorFile = %q, want %q", got, mdPath)
	}

	mdPath2 := filepath.Join(dir, "b-go.md")
	if err := writeEducatorMeta(mdPath2, educatorMeta{Course: "b", Language: "go", Version: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := findEducatorFile(dir); err == nil {
		t.Error("ambiguous dir: want error, got nil")
	}
}

func TestEducatorCmdDispatch(t *testing.T) {
	if err := educatorCmd(nil); err == nil {
		t.Error("want error for no subcommand")
	}
	if err := educatorCmd([]string{"bogus"}); err == nil {
		t.Error("want error for unknown subcommand")
	}
}

// TestEducatorAliasRouting checks main's run() dispatch wires both
// "educator" and "ed" to the same subcommand handling (issue #43/#44/#45's
// alias requirement), using the "no subcommand" signal (educatorCmd prints
// the educator help and returns errHelpShown) as the observable evidence
// that dispatch reached educatorCmd rather than falling through to an
// unknown-command error.
func TestEducatorAliasRouting(t *testing.T) {
	for _, alias := range []string{"educator", "ed"} {
		err := run([]string{alias})
		if !errors.Is(err, errHelpShown) {
			t.Errorf("run([%q]) = %v, want errHelpShown", alias, err)
		}
	}
}

func init() {
	// Guard against a future rename of validCourseMD accidentally producing
	// a fixture ingest.Parse rejects, which would make every test above
	// fail for the wrong reason.
	if probs := lintSource([]byte(validCourseMD)); len(probs) != 0 {
		panic(fmt.Sprintf("validCourseMD fixture is invalid: %+v", probs))
	}
}
