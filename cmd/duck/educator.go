package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/michael-duren/rubber-duck/internal/ingest"
)

// educatorCmd dispatches `duck educator`/`duck ed` subcommands. Unlike
// `duck pull`/`test`/`submit` (the learner flow, scaffolding one directory
// per challenge), this family round-trips a single course-variant markdown
// document with the /api/v1 variant endpoints, which issue #42 made
// author-usable (gc_u_ user-token auth + optimistic concurrency) — same
// bearer-token auth as `duck submit` (loadToken), no separate "author"
// credential.
func educatorCmd(args []string) error {
	if len(args) == 0 {
		return errEducatorUsage
	}
	switch args[0] {
	case "pull":
		return educatorPullCmd(args[1:])
	case "push":
		return educatorPushCmd(args[1:])
	case "lint":
		return educatorLintCmd(args[1:])
	default:
		return errEducatorUsage
	}
}

var errEducatorUsage = errors.New("usage: duck educator <pull|push|lint> [args]")

// educatorMetaSuffix names the sidecar `duck educator pull` writes next to
// the fetched markdown file, e.g. "intro-to-go-go.md" gets a sidecar named
// "intro-to-go-go.md.meta.json". Sidecars are per-file (not one
// directory-wide file like the learner flow's .duck-course.json) so several
// pulled course variants can live in the same directory without clobbering
// each other's metadata.
const educatorMetaSuffix = ".meta.json"

// educatorMeta is written by `duck educator pull` alongside the fetched
// markdown file so `duck educator push` knows where to PUT it back to and
// can enforce optimistic concurrency (expected_version) without the user
// re-typing the server URL/course/language every time.
type educatorMeta struct {
	BaseURL  string `json:"base_url"`
	Course   string `json:"course"`
	Language string `json:"language"`
	Version  int    `json:"version"`
}

func writeEducatorMeta(mdPath string, m educatorMeta) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(mdPath+educatorMetaSuffix, b, 0o644)
}

func readEducatorMeta(mdPath string) (educatorMeta, error) {
	b, err := os.ReadFile(mdPath + educatorMetaSuffix)
	if err != nil {
		return educatorMeta{}, fmt.Errorf("no sidecar for %s (run `duck educator pull` first): %w", mdPath, err)
	}
	var m educatorMeta
	if err := json.Unmarshal(b, &m); err != nil {
		return educatorMeta{}, fmt.Errorf("parse %s%s: %w", mdPath, educatorMetaSuffix, err)
	}
	// The sidecar is a user-editable file: catch a hand-edited or truncated
	// one here, where the bad data enters, rather than failing later with an
	// opaque HTTP error against a malformed URL. Version needs no check — a
	// bogus 0 already yields a clean version conflict.
	if m.BaseURL == "" || m.Course == "" || m.Language == "" {
		return educatorMeta{}, fmt.Errorf("sidecar %s%s is missing base_url/course/language — re-run `duck educator pull`", mdPath, educatorMetaSuffix)
	}
	return m, nil
}

// findEducatorFile looks in dir for exactly one educator sidecar left by
// `duck educator pull` and returns the markdown file it describes, so
// `push`/`lint` can be run with no arguments right after a pull. Zero or
// more-than-one matches require an explicit file argument instead.
func findEducatorFile(dir string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*"+educatorMetaSuffix))
	if err != nil {
		return "", err
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no educator sidecar found in %s (run `duck educator pull` first, or pass a file path)", dir)
	case 1:
		return strings.TrimSuffix(matches[0], educatorMetaSuffix), nil
	default:
		return "", fmt.Errorf("multiple educator sidecars found in %s — pass a file path to disambiguate", dir)
	}
}

// resolveEducatorFile returns the explicit file argument if one was given,
// otherwise locates it via findEducatorFile in the current directory.
func resolveEducatorFile(rest []string) (string, error) {
	if len(rest) == 1 {
		return rest[0], nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return findEducatorFile(cwd)
}

// lintProblem is the line-numbered shape shared by internal/ingest's local
// validation and the server's 422 invalid_course_markdown error details, so
// printProblems can render both identically.
type lintProblem struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}

// lintSource runs internal/ingest.Parse locally (no network round trip) and
// returns the problems found, or nil if the document is valid. Shared by
// `duck educator lint` and the pre-flight check in `duck educator push`.
func lintSource(src []byte) []lintProblem {
	_, err := ingest.Parse(src)
	if err == nil {
		return nil
	}
	verr, ok := errors.AsType[*ingest.ValidationError](err)
	if !ok {
		// Not a validation error (shouldn't happen for well-formed input,
		// but surface it rather than silently reporting "valid").
		return []lintProblem{{Message: err.Error()}}
	}
	probs := make([]lintProblem, len(verr.Problems))
	for i, p := range verr.Problems {
		probs[i] = lintProblem{Line: p.Line, Message: p.Message}
	}
	return probs
}

// printProblems prints line-numbered validation problems in one format,
// used whether they came from a local `duck educator lint` run or from the
// server's 422 response to `duck educator push` — the output looks
// identical either way.
func printProblems(path string, probs []lintProblem) {
	fmt.Printf("%s: %d problem(s) found\n", path, len(probs))
	for _, p := range probs {
		if p.Line > 0 {
			fmt.Printf("  line %d: %s\n", p.Line, p.Message)
		} else {
			// Document-level problems (and the server's omitempty line field)
			// carry no line number; "line 0" would point nowhere.
			fmt.Printf("  %s\n", p.Message)
		}
	}
}

// educatorPullCmd fetches a course variant's markdown (GET
// /api/v1/courses/{slug}/variants/{language}, bearer-token auth required)
// and writes it to a single local file plus its version sidecar.
func educatorPullCmd(args []string) error {
	fs := flag.NewFlagSet("educator pull", flag.ContinueOnError)
	base := fs.String("base", envOr("DUCK_BASE_URL", "http://localhost:8080"), "server base URL")
	force := fs.Bool("force", false, "overwrite a local file whose content differs from the server's")
	rest, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: duck educator pull <course>/<language> [--base URL] [--force]")
	}
	course, language, ok := strings.Cut(rest[0], "/")
	if !ok || course == "" || language == "" {
		return fmt.Errorf("usage: duck educator pull <course>/<language>")
	}

	token, err := loadToken()
	if err != nil {
		return err
	}

	baseURL := strings.TrimRight(*base, "/")
	getURL := baseURL + "/api/v1/courses/" + url.PathEscape(course) + "/variants/" + url.PathEscape(language)
	req, err := http.NewRequest(http.MethodGet, getURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch variant: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("unauthorized: token missing or revoked")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch variant: server said %s: %s", resp.Status, body)
	}

	var payload struct {
		Markdown string `json:"markdown"`
		Version  int    `json:"version"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	// Zero values here mean a response without those keys (e.g. a server
	// older than this CLI whose GET is markdown-only). Silently accepting
	// them would write an empty file and a version-0 sidecar whose every
	// future push reports a bogus conflict.
	if payload.Markdown == "" || payload.Version < 1 {
		return fmt.Errorf("server response is missing markdown or version — the server may be older than this duck CLI")
	}

	mdPath := course + "-" + language + ".md"
	// Never silently eat local edits: a differing existing file may hold
	// unpushed work (the exact situation a version-conflicted push sends
	// people here from).
	if existing, err := os.ReadFile(mdPath); err == nil && string(existing) != payload.Markdown && !*force {
		return fmt.Errorf("%s already exists and differs from the server copy — refusing to overwrite it; save your local edits elsewhere, then re-run with --force", mdPath)
	}
	if err := os.WriteFile(mdPath, []byte(payload.Markdown), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", mdPath, err)
	}
	if err := writeEducatorMeta(mdPath, educatorMeta{
		BaseURL: baseURL, Course: course, Language: language, Version: payload.Version,
	}); err != nil {
		return fmt.Errorf("write sidecar: %w", err)
	}

	fmt.Printf("pulled %s (version %d)\n", mdPath, payload.Version)
	return nil
}

// educatorPushCmd sends a local course-variant markdown file back with PUT
// /api/v1/courses/{slug}/variants/{language}, using the sidecar's recorded
// expected_version for optimistic concurrency.
func educatorPushCmd(args []string) error {
	fs := flag.NewFlagSet("educator push", flag.ContinueOnError)
	rest, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	if len(rest) > 1 {
		return fmt.Errorf("usage: duck educator push [file]")
	}
	mdPath, err := resolveEducatorFile(rest)
	if err != nil {
		return err
	}

	meta, err := readEducatorMeta(mdPath)
	if err != nil {
		return err
	}

	src, err := os.ReadFile(mdPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", mdPath, err)
	}

	// Cheap local pre-flight before spending a network round trip: same
	// checks the server would run, same output shape either way.
	if probs := lintSource(src); len(probs) > 0 {
		printProblems(mdPath, probs)
		return fmt.Errorf("%d problem(s) found — fix before pushing", len(probs))
	}

	token, err := loadToken()
	if err != nil {
		return err
	}

	expectedVersion := meta.Version
	reqBody, err := json.Marshal(struct {
		Markdown        string `json:"markdown"`
		ExpectedVersion *int   `json:"expected_version,omitempty"`
	}{Markdown: string(src), ExpectedVersion: &expectedVersion})
	if err != nil {
		return err
	}

	baseURL := strings.TrimRight(meta.BaseURL, "/")
	putURL := baseURL + "/api/v1/courses/" + url.PathEscape(meta.Course) + "/variants/" + url.PathEscape(meta.Language)
	req, err := http.NewRequest(http.MethodPut, putURL, bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("push: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("unauthorized: token missing or revoked")
	}

	var errResp struct {
		Error struct {
			Code    string        `json:"code"`
			Message string        `json:"message"`
			Details []lintProblem `json:"details"`
		} `json:"error"`
	}

	if resp.StatusCode == http.StatusConflict {
		_ = json.Unmarshal(body, &errResp)
		if errResp.Error.Code == "version_conflict" {
			// Careful with this wording: plain `duck educator pull` would
			// overwrite exactly the edits the user is trying to push, so
			// tell them to save those edits first (pull refuses to clobber
			// a differing file without --force, as a backstop).
			return fmt.Errorf("someone else changed this course variant since you last pulled it — save your edits elsewhere, run `duck educator pull --force` to fetch the latest version, then reapply them and push again")
		}
		if errResp.Error.Message != "" {
			// e.g. slug_mismatch when frontmatter was edited to disagree
			// with the sidecar — the server's message is human-readable.
			return fmt.Errorf("push: %s: %s", resp.Status, errResp.Error.Message)
		}
		return fmt.Errorf("push: server said %s: %s", resp.Status, body)
	}
	if resp.StatusCode == http.StatusUnprocessableEntity {
		if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Code == "invalid_course_markdown" {
			printProblems(mdPath, errResp.Error.Details)
			return fmt.Errorf("%d problem(s) found — fix before pushing", len(errResp.Error.Details))
		}
		return fmt.Errorf("push: server said %s: %s", resp.Status, body)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("push: server said %s: %s", resp.Status, body)
	}

	// Past this point the server has committed the write, so a failure is
	// local bookkeeping only — say so explicitly, or the next push's stale
	// expected_version turns it into a phantom "someone else changed this"
	// conflict with no hint of the real cause.
	var summary struct {
		Version     int `json:"version"`
		Lessons     int `json:"lessons"`
		Challenges  int `json:"challenges"`
		TotalPoints int `json:"total_points"`
	}
	if err := json.Unmarshal(body, &summary); err != nil {
		return fmt.Errorf("push succeeded on the server, but parsing its response failed: %w — run `duck educator pull` to resync the sidecar", err)
	}

	meta.Version = summary.Version
	if err := writeEducatorMeta(mdPath, meta); err != nil {
		return fmt.Errorf("push succeeded on the server (now version %d), but updating the local sidecar failed: %w — run `duck educator pull` to resync", summary.Version, err)
	}

	fmt.Printf("pushed %s — version %d (%d lessons, %d challenges, %d pts)\n",
		mdPath, summary.Version, summary.Lessons, summary.Challenges, summary.TotalPoints)
	return nil
}

// educatorLintCmd validates a local course-variant markdown file with
// internal/ingest.Parse directly — no server round trip.
func educatorLintCmd(args []string) error {
	fs := flag.NewFlagSet("educator lint", flag.ContinueOnError)
	rest, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	if len(rest) > 1 {
		return fmt.Errorf("usage: duck educator lint [file]")
	}
	mdPath, err := resolveEducatorFile(rest)
	if err != nil {
		return err
	}

	src, err := os.ReadFile(mdPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", mdPath, err)
	}

	probs := lintSource(src)
	if len(probs) == 0 {
		fmt.Printf("%s: no problems found\n", mdPath)
		return nil
	}
	printProblems(mdPath, probs)
	return fmt.Errorf("%d problem(s) found", len(probs))
}
