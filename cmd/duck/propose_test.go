package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestPropose(t *testing.T) {
	t.Setenv("DUCK_TOKEN", "gc_u_testtoken")

	t.Run("creates a proposal and records it in the sidecar", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		var gotBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/api/v1/proposals" {
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer gc_u_testtoken" {
				t.Errorf("Authorization = %q", got)
			}
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(proposalResponse{
				ID: 7, Course: "intro-to-concurrency", Language: "go",
				BaseVersion: 3, Revision: 1, Status: "open", URL: "/proposals/7",
			})
		}))
		defer srv.Close()

		// No sidecar: course/language must come from the frontmatter.
		if err := os.WriteFile("intro-to-concurrency-go.md", []byte(validCourseMD), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := proposeCmd([]string{"intro-to-concurrency-go.md", "--base", srv.URL, "--title", "Fix", "--summary", "sum"}); err != nil {
			t.Fatalf("proposeCmd: %v", err)
		}

		if gotBody["course"] != "intro-to-concurrency" || gotBody["language"] != "go" {
			t.Errorf("request body course/language = %v/%v", gotBody["course"], gotBody["language"])
		}
		if gotBody["title"] != "Fix" || gotBody["summary"] != "sum" || gotBody["markdown"] != validCourseMD {
			t.Errorf("request body content = %v", gotBody)
		}

		meta, err := readEducatorMeta("intro-to-concurrency-go.md")
		if err != nil {
			t.Fatalf("readEducatorMeta: %v", err)
		}
		if meta.ProposalID != 7 {
			t.Errorf("sidecar proposal_id = %d, want 7", meta.ProposalID)
		}
	})

	t.Run("sidecar-recorded proposal updates via PUT", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		var method, path string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			method, path = r.Method, r.URL.Path
			_ = json.NewEncoder(w).Encode(proposalResponse{
				ID: 7, Course: "intro-to-concurrency", Language: "go",
				BaseVersion: 4, Revision: 2, Status: "open", URL: "/proposals/7",
			})
		}))
		defer srv.Close()

		mdPath := pushFixture(t, dir, educatorMeta{
			BaseURL: srv.URL, Course: "intro-to-concurrency", Language: "go", Version: 3, ProposalID: 7,
		}, validCourseMD)

		if err := proposeCmd([]string{mdPath}); err != nil {
			t.Fatalf("proposeCmd: %v", err)
		}
		if method != http.MethodPut || path != "/api/v1/proposals/7" {
			t.Errorf("request = %s %s, want PUT /api/v1/proposals/7", method, path)
		}
	})

	t.Run("409 duplicate finds the existing proposal and updates it", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		var putPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost && r.URL.Path == "/api/v1/proposals":
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
					"code": "duplicate_proposal", "message": "already open",
				}})
			case r.Method == http.MethodGet && r.URL.Path == "/api/v1/proposals":
				_ = json.NewEncoder(w).Encode(map[string]any{"proposals": []proposalResponse{
					{ID: 12, Course: "intro-to-concurrency", Language: "go", Status: "open"},
				}})
			case r.Method == http.MethodPut:
				putPath = r.URL.Path
				_ = json.NewEncoder(w).Encode(proposalResponse{
					ID: 12, Course: "intro-to-concurrency", Language: "go",
					Revision: 2, Status: "open", URL: "/proposals/12",
				})
			default:
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
		}))
		defer srv.Close()

		if err := os.WriteFile("intro-to-concurrency-go.md", []byte(validCourseMD), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := proposeCmd([]string{"intro-to-concurrency-go.md", "--base", srv.URL}); err != nil {
			t.Fatalf("proposeCmd: %v", err)
		}
		if putPath != "/api/v1/proposals/12" {
			t.Errorf("update path = %q, want /api/v1/proposals/12", putPath)
		}
		meta, err := readEducatorMeta("intro-to-concurrency-go.md")
		if err != nil || meta.ProposalID != 12 {
			t.Errorf("sidecar proposal_id = %d (%v), want 12", meta.ProposalID, err)
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

		if err := os.WriteFile("bad.md", []byte("not a course document"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := proposeCmd([]string{"bad.md", "--base", srv.URL}); err == nil {
			t.Fatal("want error for locally-invalid markdown")
		}
		if called {
			t.Error("server should not have been called when local lint fails")
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

		if err := os.WriteFile("intro-to-concurrency-go.md", []byte(validCourseMD), 0o644); err != nil {
			t.Fatal(err)
		}
		err := proposeCmd([]string{"intro-to-concurrency-go.md", "--base", srv.URL})
		if err == nil || !strings.Contains(err.Error(), "1 problem") {
			t.Fatalf("proposeCmd err = %v, want a problem-count error", err)
		}
	})

	t.Run("unauthorized names the token source", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "unauthorized", "message": "nope"}})
		}))
		defer srv.Close()

		if err := os.WriteFile("intro-to-concurrency-go.md", []byte(validCourseMD), 0o644); err != nil {
			t.Fatal(err)
		}
		err := proposeCmd([]string{"intro-to-concurrency-go.md", "--base", srv.URL})
		if err == nil || !strings.Contains(err.Error(), "unauthorized") {
			t.Fatalf("proposeCmd err = %v, want unauthorized error", err)
		}
	})
}

func TestEducatorPushRetired(t *testing.T) {
	err := educatorCmd([]string{"push"})
	if err == nil || !strings.Contains(err.Error(), "duck propose") {
		t.Fatalf("educator push err = %v, want the duck-propose replacement message", err)
	}
}

func TestProposalsList(t *testing.T) {
	t.Setenv("DUCK_TOKEN", "gc_u_testtoken")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/proposals":
			if r.URL.Query().Get("mine") != "1" {
				t.Errorf("expected mine=1, got %q", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"proposals": []proposalResponse{
				{ID: 3, Course: "intro-to-go", Language: "go", Title: "T", Status: "open", Approvals: 1, Stale: true, URL: "/proposals/3"},
			}})
		case "/api/v1/proposals/3":
			_ = json.NewEncoder(w).Encode(proposalResponse{
				ID: 3, Course: "intro-to-go", Language: "go", Title: "T", Status: "published", URL: "/proposals/3",
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	if err := proposalsCmd([]string{"--base", srv.URL}); err != nil {
		t.Fatalf("proposalsCmd: %v", err)
	}
	if err := proposalsCmd([]string{"status", "3", "--base", srv.URL}); err != nil {
		t.Fatalf("proposals status: %v", err)
	}
	if err := proposalsCmd([]string{"status", "notanumber", "--base", srv.URL}); err == nil {
		t.Fatal("want error for non-numeric id")
	}
}
