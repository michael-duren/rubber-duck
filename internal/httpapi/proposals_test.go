package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/michael-duren/rubber-duck/internal/domain"
)

func TestProposalAPIAuth(t *testing.T) {
	mux, _ := testAPI(t)
	doc := seedDoc(t)

	cases := []struct {
		name   string
		bearer string
	}{
		{"no token", ""},
		{"old agent key", "gc_notauserkey"},
		{"unknown user token", "gc_u_nobody"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := doJSONAs(mux, "POST", "/api/v1/proposals", c.bearer, map[string]string{"markdown": doc})
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", rec.Code)
			}
		})
	}
}

func TestProposalCreate(t *testing.T) {
	doc := seedDoc(t)

	t.Run("create returns 201 with base version and url", func(t *testing.T) {
		mux, fs := testAPI(t)
		fs.addUser("gc_u_alice", domain.User{ID: 42, Username: "alice"})
		fs.seedVariant(t, doc)

		rec := doJSONAs(mux, "POST", "/api/v1/proposals", "gc_u_alice",
			map[string]string{"markdown": doc, "title": "Fix typos", "summary": "spelling"})
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d body %s", rec.Code, rec.Body)
		}
		var got map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if got["course"] != "intro-to-concurrency" || got["language"] != "go" ||
			got["base_version"].(float64) != 1 || got["status"] != "open" ||
			got["url"] != "/proposals/1" {
			t.Errorf("response = %v", got)
		}
	})

	t.Run("duplicate open proposal is 409", func(t *testing.T) {
		mux, fs := testAPI(t)
		fs.addUser("gc_u_alice", domain.User{ID: 42, Username: "alice"})

		if rec := doJSONAs(mux, "POST", "/api/v1/proposals", "gc_u_alice", map[string]string{"markdown": doc}); rec.Code != http.StatusCreated {
			t.Fatalf("first = %d", rec.Code)
		}
		rec := doJSONAs(mux, "POST", "/api/v1/proposals", "gc_u_alice", map[string]string{"markdown": doc})
		if rec.Code != http.StatusConflict {
			t.Fatalf("second = %d, want 409", rec.Code)
		}
		var resp struct {
			Error struct{ Code string }
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Error.Code != "duplicate_proposal" {
			t.Errorf("code = %q", resp.Error.Code)
		}
	})

	t.Run("invalid markdown is 422 with line details and no proposal", func(t *testing.T) {
		mux, fs := testAPI(t)
		fs.addUser("gc_u_alice", domain.User{ID: 42, Username: "alice"})

		bad := strings.Replace(doc, "### Tests", "### Test", 1)
		rec := doJSONAs(mux, "POST", "/api/v1/proposals", "gc_u_alice", map[string]string{"markdown": bad})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d body %s", rec.Code, rec.Body)
		}
		var resp struct {
			Error struct {
				Code    string
				Details []struct {
					Line    int
					Message string
				}
			}
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Error.Code != "invalid_course_markdown" || len(resp.Error.Details) == 0 || resp.Error.Details[0].Line == 0 {
			t.Errorf("error = %+v", resp.Error)
		}
		if len(fs.proposals) != 0 {
			t.Errorf("invalid document must not create a proposal")
		}
	})

	t.Run("body course/language mismatching frontmatter is 409", func(t *testing.T) {
		mux, fs := testAPI(t)
		fs.addUser("gc_u_alice", domain.User{ID: 42, Username: "alice"})

		rec := doJSONAs(mux, "POST", "/api/v1/proposals", "gc_u_alice",
			map[string]string{"markdown": doc, "course": "some-other-course"})
		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409", rec.Code)
		}
		var resp struct {
			Error struct{ Code string }
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Error.Code != "slug_mismatch" {
			t.Errorf("code = %q", resp.Error.Code)
		}
	})

	t.Run("bad d2 diagram is 422, not a 500", func(t *testing.T) {
		mux, fs := testAPI(t)
		fs.addUser("gc_u_alice", domain.User{ID: 42, Username: "alice"})
		// Parses fine (ingest doesn't compile diagrams) but the ```d2
		// fence fails to compile when the render check runs ToDomain.
		badD2 := "---\ncourse: c\ntitle: T\nlanguage: go\ndescription: d\n---\n\n" +
			"# Lesson: One {#one}\n\n```d2\nx -> \n```\n\n" +
			"## Challenge: A {#a points=5}\n\nPrompt.\n\n" +
			"### Starter\n\n```go\ncode\n```\n\n### Tests\n\n```go\ntests\n```\n\n" +
			"# Final Challenge: F {#fin points=9}\n\n" +
			"### Starter\n\n```go\ns\n```\n\n### Tests\n\n```go\nt\n```\n"
		rec := doJSONAs(mux, "POST", "/api/v1/proposals", "gc_u_alice", map[string]string{"markdown": badD2})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d body %s, want 422", rec.Code, rec.Body)
		}
		var resp struct {
			Error struct{ Code, Message string }
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Error.Code != "invalid_course_markdown" || !strings.Contains(resp.Error.Message, "d2") {
			t.Errorf("error = %+v, want invalid_course_markdown mentioning d2", resp.Error)
		}
		if len(fs.proposals) != 0 {
			t.Errorf("bad-diagram document must not create a proposal")
		}
	})

	t.Run("empty markdown is 400", func(t *testing.T) {
		mux, fs := testAPI(t)
		fs.addUser("gc_u_alice", domain.User{ID: 42, Username: "alice"})
		rec := doJSONAs(mux, "POST", "/api/v1/proposals", "gc_u_alice", map[string]string{"markdown": ""})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("oversized body is 413", func(t *testing.T) {
		mux, fs := testAPI(t)
		fs.addUser("gc_u_alice", domain.User{ID: 42, Username: "alice"})
		rec := doJSONAs(mux, "POST", "/api/v1/proposals", "gc_u_alice",
			map[string]string{"markdown": strings.Repeat("a", maxDocumentBytes)})
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("status = %d, want 413", rec.Code)
		}
	})
}

func TestProposalUpdateGetWithdraw(t *testing.T) {
	doc := seedDoc(t)

	setup := func(t *testing.T) (*http.ServeMux, *fakeStore) {
		mux, fs := testAPI(t)
		fs.addUser("gc_u_alice", domain.User{ID: 42, Username: "alice"})
		fs.addUser("gc_u_bob", domain.User{ID: 7, Username: "bob"})
		if rec := doJSONAs(mux, "POST", "/api/v1/proposals", "gc_u_alice", map[string]string{"markdown": doc}); rec.Code != http.StatusCreated {
			t.Fatalf("seed proposal = %d", rec.Code)
		}
		return mux, fs
	}

	t.Run("proposer update bumps revision", func(t *testing.T) {
		mux, _ := setup(t)
		rec := doJSONAs(mux, "PUT", "/api/v1/proposals/1", "gc_u_alice",
			map[string]string{"markdown": doc + "\n<!-- v2 -->"})
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body %s", rec.Code, rec.Body)
		}
		var got map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if got["revision"].(float64) != 2 {
			t.Errorf("revision = %v, want 2", got["revision"])
		}
	})

	t.Run("update by non-proposer is 404", func(t *testing.T) {
		mux, _ := setup(t)
		rec := doJSONAs(mux, "PUT", "/api/v1/proposals/1", "gc_u_bob",
			map[string]string{"markdown": doc + "\n<!-- bob -->"})
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("update that retargets another variant is 409", func(t *testing.T) {
		mux, _ := setup(t)
		moved := strings.Replace(doc, "course: intro-to-concurrency", "course: some-other-course", 1)
		if moved == doc {
			t.Fatal("fixture frontmatter changed; update this test's replacement")
		}
		rec := doJSONAs(mux, "PUT", "/api/v1/proposals/1", "gc_u_alice",
			map[string]string{"markdown": moved})
		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d body %s, want 409", rec.Code, rec.Body)
		}
		var got struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if got.Error.Code != "variant_mismatch" {
			t.Errorf("error code = %q, want variant_mismatch", got.Error.Code)
		}
	})

	t.Run("any authenticated user can read a proposal with its document", func(t *testing.T) {
		mux, _ := setup(t)
		rec := doJSONAs(mux, "GET", "/api/v1/proposals/1", "gc_u_bob", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		var got map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if got["markdown"] != doc {
			t.Errorf("get did not include the document")
		}
	})

	t.Run("list mine only lists mine", func(t *testing.T) {
		mux, _ := setup(t)
		rec := doJSONAs(mux, "GET", "/api/v1/proposals", "gc_u_bob", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		var got struct {
			Proposals []any `json:"proposals"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if len(got.Proposals) != 0 {
			t.Errorf("bob sees %d proposals, want 0", len(got.Proposals))
		}
		rec = doJSONAs(mux, "GET", "/api/v1/proposals", "gc_u_alice", nil)
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if len(got.Proposals) != 1 {
			t.Errorf("alice sees %d proposals, want 1", len(got.Proposals))
		}
	})

	t.Run("withdraw then update is closed conflict", func(t *testing.T) {
		mux, _ := setup(t)
		if rec := doJSONAs(mux, "POST", "/api/v1/proposals/1/withdraw", "gc_u_alice", nil); rec.Code != http.StatusNoContent {
			t.Fatalf("withdraw = %d", rec.Code)
		}
		if rec := doJSONAs(mux, "POST", "/api/v1/proposals/1/withdraw", "gc_u_alice", nil); rec.Code != http.StatusConflict {
			t.Errorf("double withdraw = %d, want 409", rec.Code)
		}
		rec := doJSONAs(mux, "PUT", "/api/v1/proposals/1", "gc_u_alice", map[string]string{"markdown": doc})
		if rec.Code != http.StatusConflict {
			t.Errorf("update closed = %d, want 409", rec.Code)
		}
	})

	t.Run("withdraw by non-proposer is 404", func(t *testing.T) {
		mux, _ := setup(t)
		if rec := doJSONAs(mux, "POST", "/api/v1/proposals/1/withdraw", "gc_u_bob", nil); rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
	})
}
