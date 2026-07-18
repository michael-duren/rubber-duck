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

	t.Run("gone sidecar proposal falls back to POST", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		var calls []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, r.Method+" "+r.URL.Path)
			switch {
			case r.Method == http.MethodPut && r.URL.Path == "/api/v1/proposals/7":
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
					"code": "not_found", "message": "no such proposal",
				}})
			case r.Method == http.MethodPost && r.URL.Path == "/api/v1/proposals":
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(proposalResponse{
					ID: 20, Course: "intro-to-concurrency", Language: "go",
					Revision: 1, Status: "open", URL: "/proposals/20",
				})
			default:
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
		}))
		defer srv.Close()

		mdPath := pushFixture(t, dir, educatorMeta{
			BaseURL: srv.URL, Course: "intro-to-concurrency", Language: "go", Version: 3, ProposalID: 7,
		}, validCourseMD)

		if err := proposeCmd([]string{mdPath}); err != nil {
			t.Fatalf("proposeCmd: %v", err)
		}
		want := []string{"PUT /api/v1/proposals/7", "POST /api/v1/proposals"}
		if len(calls) != 2 || calls[0] != want[0] || calls[1] != want[1] {
			t.Errorf("calls = %v, want %v", calls, want)
		}
		if meta, err := readEducatorMeta(mdPath); err != nil || meta.ProposalID != 20 {
			t.Errorf("sidecar proposal_id = %d (%v), want 20", meta.ProposalID, err)
		}
	})

	t.Run("closed sidecar proposal recovers into another open proposal", func(t *testing.T) {
		// The remembered proposal closed (published), but the user meanwhile
		// opened proposal 12 for the same variant in the web editor: the
		// fallback POST 409s and must find-and-update 12, not die.
		dir := t.TempDir()
		t.Chdir(dir)

		var putPaths []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPut && r.URL.Path == "/api/v1/proposals/7":
				putPaths = append(putPaths, r.URL.Path)
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
					"code": "proposal_closed", "message": "no longer open",
				}})
			case r.Method == http.MethodPost && r.URL.Path == "/api/v1/proposals":
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
					"code": "duplicate_proposal", "message": "already open",
				}})
			case r.Method == http.MethodGet && r.URL.Path == "/api/v1/proposals":
				_ = json.NewEncoder(w).Encode(map[string]any{"proposals": []proposalResponse{
					{ID: 12, Course: "intro-to-concurrency", Language: "go", Status: "open"},
				}})
			case r.Method == http.MethodPut && r.URL.Path == "/api/v1/proposals/12":
				putPaths = append(putPaths, r.URL.Path)
				_ = json.NewEncoder(w).Encode(proposalResponse{
					ID: 12, Course: "intro-to-concurrency", Language: "go",
					Revision: 2, Status: "open", URL: "/proposals/12",
				})
			default:
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
		}))
		defer srv.Close()

		mdPath := pushFixture(t, dir, educatorMeta{
			BaseURL: srv.URL, Course: "intro-to-concurrency", Language: "go", Version: 3, ProposalID: 7,
		}, validCourseMD)

		if err := proposeCmd([]string{mdPath}); err != nil {
			t.Fatalf("proposeCmd: %v", err)
		}
		if len(putPaths) != 2 || putPaths[1] != "/api/v1/proposals/12" {
			t.Errorf("PUTs = %v, want the recovery to update proposal 12", putPaths)
		}
		if meta, err := readEducatorMeta(mdPath); err != nil || meta.ProposalID != 12 {
			t.Errorf("sidecar proposal_id = %d (%v), want 12", meta.ProposalID, err)
		}
	})

	t.Run("explicit --base overrides the sidecar server", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)

		other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("sidecar server must not be called, got %s %s", r.Method, r.URL.Path)
		}))
		defer other.Close()

		var got string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = r.Method + " " + r.URL.Path
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(proposalResponse{
				ID: 1, Course: "intro-to-concurrency", Language: "go",
				Revision: 1, Status: "open", URL: "/proposals/1",
			})
		}))
		defer srv.Close()

		// Sidecar remembers proposal 7 on the OTHER server; --base must win,
		// and the remembered id (meaningless on this server) must be dropped
		// in favor of a fresh POST.
		mdPath := pushFixture(t, dir, educatorMeta{
			BaseURL: other.URL, Course: "intro-to-concurrency", Language: "go", Version: 3, ProposalID: 7,
		}, validCourseMD)

		if err := proposeCmd([]string{mdPath, "--base", srv.URL}); err != nil {
			t.Fatalf("proposeCmd: %v", err)
		}
		if got != "POST /api/v1/proposals" {
			t.Errorf("request = %q, want POST to the --base server", got)
		}
		meta, err := readEducatorMeta(mdPath)
		if err != nil || meta.BaseURL != srv.URL || meta.ProposalID != 1 {
			t.Errorf("sidecar = %+v (%v), want re-pinned to the --base server with the new proposal", meta, err)
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

	// An empty list is a normal state, not an error.
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"proposals": []proposalResponse{}})
	}))
	defer empty.Close()
	if err := proposalsCmd([]string{"--base", empty.URL}); err != nil {
		t.Fatalf("empty proposals list: %v", err)
	}
}
