package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// dsaLikeChallenges mirrors a real course shape: two lessons sharing a
// number-worthy structure, a lesson (kahns-algorithm-style) with no
// challenge between lesson 3 and the final, and the final itself.
var dsaLikeChallenges = []challengeJSON{
	{LessonSlug: "dynamic-array", LessonNumber: 1, Slug: "growable-array", StarterCode: "s", TestCode: "t"},
	{LessonSlug: "divide-and-conquer", LessonNumber: 3, Slug: "merge", StarterCode: "s", TestCode: "t"},
	{LessonSlug: "divide-and-conquer", LessonNumber: 3, Slug: "mergesort", StarterCode: "s", TestCode: "t"},
	{LessonSlug: "", LessonNumber: 0, Slug: "task-scheduler", StarterCode: "s", TestCode: "t"},
}

func challengesServer(t *testing.T, challenges []challengeJSON) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/courses/dsa/variants/c/challenges" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"challenges": challenges})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPullOrdersDirsByLesson(t *testing.T) {
	t.Chdir(t.TempDir())
	srv := challengesServer(t, dsaLikeChallenges)

	if err := pullCmd([]string{"dsa/c", "--base", srv.URL}); err != nil {
		t.Fatal(err)
	}

	want := []string{"01-growable-array", "03a-merge", "03b-mergesort", "final-task-scheduler"}
	for _, dir := range want {
		if _, err := os.Stat(filepath.Join("dsa-c", dir, "solution.c")); err != nil {
			t.Errorf("missing %s/solution.c: %v", dir, err)
		}
	}
	entries, err := os.ReadDir("dsa-c")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		if e.IsDir() {
			got = append(got, e.Name()) // ReadDir sorts by name: course order
		}
	}
	if len(got) != len(want) {
		t.Fatalf("dirs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("dir[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// A server without lesson_number (all zero) must still produce ordered
// prefixes, derived from lesson transitions.
func TestPullDerivesNumbersFromOldServer(t *testing.T) {
	t.Chdir(t.TempDir())
	old := make([]challengeJSON, len(dsaLikeChallenges))
	copy(old, dsaLikeChallenges)
	for i := range old {
		old[i].LessonNumber = 0
	}
	srv := challengesServer(t, old)

	if err := pullCmd([]string{"dsa/c", "--base", srv.URL}); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"01-growable-array", "02a-merge", "02b-mergesort", "final-task-scheduler"} {
		if _, err := os.Stat(filepath.Join("dsa-c", dir)); err != nil {
			t.Errorf("missing %s: %v", dir, err)
		}
	}
}

// Re-pulling over a tree scaffolded before ordered names must not create
// duplicate directories next to the in-progress bare-slug ones.
func TestPullSkipsLegacyDirs(t *testing.T) {
	t.Chdir(t.TempDir())
	srv := challengesServer(t, dsaLikeChallenges)

	if err := os.MkdirAll(filepath.Join("dsa-c", "merge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := pullCmd([]string{"dsa/c", "--base", srv.URL}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join("dsa-c", "03a-merge")); err == nil {
		t.Error("03a-merge was created despite legacy merge/ existing")
	}
	if _, err := os.Stat(filepath.Join("dsa-c", "03b-mergesort")); err != nil {
		t.Errorf("missing 03b-mergesort: %v", err)
	}
}

func TestChallengeSlug(t *testing.T) {
	tests := []struct {
		dir, want string
	}{
		{"01-growable-array", "growable-array"},
		{"03-merge", "merge"},
		{"05a-min-heap", "min-heap"},
		{"05b-heapsort", "heapsort"},
		{"final-task-scheduler", "task-scheduler"},
		{"growable-array", "growable-array"},   // legacy layout: no prefix
		{"3-way-partition", "3-way-partition"}, // single digit: a slug, not our prefix
		{"final", "final"},                     // no dash after "final": a slug
		{"10ab-cd", "10ab-cd"},                 // two letters: a slug, not our prefix
	}
	for _, tt := range tests {
		if got := challengeSlug(tt.dir); got != tt.want {
			t.Errorf("challengeSlug(%q) = %q, want %q", tt.dir, got, tt.want)
		}
	}
}

func TestResolveChallenge(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"01-growable-array", "05a-min-heap", "final-task-scheduler", "legacy-slug"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		arg, wantDir, wantSlug string
	}{
		{"growable-array", "01-growable-array", "growable-array"},
		{"01-growable-array", "01-growable-array", "growable-array"},
		{"min-heap", "05a-min-heap", "min-heap"},
		{"task-scheduler", "final-task-scheduler", "task-scheduler"},
		{"legacy-slug", "legacy-slug", "legacy-slug"},
		{"01-growable-array/", "01-growable-array", "growable-array"}, // tab completion's trailing slash
		{"./05a-min-heap", "05a-min-heap", "min-heap"},
	}
	for _, tt := range tests {
		dir, slug, err := resolveChallenge(root, tt.arg)
		if err != nil {
			t.Errorf("resolveChallenge(%q): %v", tt.arg, err)
			continue
		}
		if dir != tt.wantDir || slug != tt.wantSlug {
			t.Errorf("resolveChallenge(%q) = %q, %q, want %q, %q", tt.arg, dir, slug, tt.wantDir, tt.wantSlug)
		}
	}

	if _, _, err := resolveChallenge(root, "nope"); err == nil {
		t.Error("resolveChallenge(nope) succeeded, want error")
	}
}
