package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/michael-duren/rubber-duck/internal/ingest"
)

// proposalResponse is the API shape of one proposal, shared by
// `duck propose` (create/update responses) and `duck proposals` (lists).
type proposalResponse struct {
	ID          int64  `json:"id"`
	Course      string `json:"course"`
	Language    string `json:"language"`
	Path        string `json:"path"` // set instead of course/language for learning-path proposals
	Title       string `json:"title"`
	BaseVersion int    `json:"base_version"`
	Revision    int    `json:"revision"`
	Status      string `json:"status"`
	Approvals   int    `json:"approvals"`
	Stale       bool   `json:"stale"`
	URL         string `json:"url"`
}

// proposeCmd submits a local course-variant markdown file as a proposal
// (POST /api/v1/proposals) for the community to review — the replacement
// for the retired `duck educator push`, which published directly. The
// course/language come from the document's frontmatter, so it works with
// or without an `educator pull` sidecar (a brand-new course has neither a
// sidecar nor a server-side variant). If the server reports you already
// have an open proposal for this variant — or the sidecar remembers one —
// the content updates that proposal in place (PUT) instead of failing.
func proposeCmd(args []string) error {
	fs := flag.NewFlagSet("propose", flag.ContinueOnError)
	base := fs.String("base", envOr("DUCK_BASE_URL", "https://duckgc.com"), "server base URL")
	title := fs.String("title", "", "proposal title (default: \"Update <course>/<language>\")")
	summary := fs.String("summary", "", "what changed and why — shown to reviewers")
	rest, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	if len(rest) > 1 {
		return fmt.Errorf("usage: duck propose [file] [--base URL] [--title T] [--summary S]")
	}
	// An explicit --base must win over the sidecar's remembered server —
	// otherwise a user pointing at a dev server would silently send the
	// proposal (and their token) wherever the file was last pulled from.
	baseSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "base" {
			baseSet = true
		}
	})
	mdPath, err := resolveEducatorFile(rest)
	if err != nil {
		return err
	}

	src, err := os.ReadFile(mdPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", mdPath, err)
	}

	// Cheap local pre-flight before spending a network round trip: same
	// checks the server would run, same output shape either way. A "path:"
	// frontmatter key makes this a learning-path proposal; anything else is
	// a course variant.
	var course, language, pathSlug, target string
	if ingest.IsPathDocument(src) {
		res, probs := lintParsePath(src)
		if len(probs) > 0 {
			printProblems(mdPath, probs)
			return fmt.Errorf("%d problem(s) found — fix before proposing", len(probs))
		}
		pathSlug = res.Path.Path
		target = "path " + pathSlug
	} else {
		res, probs := lintParse(src)
		if len(probs) > 0 {
			printProblems(mdPath, probs)
			return fmt.Errorf("%d problem(s) found — fix before proposing", len(probs))
		}
		course, language = res.Course.Course, res.Course.Language
		target = course + "/" + language
	}

	// A sidecar is optional here, but when present it pins the server and
	// remembers the open proposal this file previously created. Path
	// documents have no pull flow and so no sidecar — the duplicate-recovery
	// below still finds an existing open path proposal.
	baseURL := strings.TrimRight(*base, "/")
	var meta educatorMeta
	haveMeta := false
	if m, err := readEducatorMeta(mdPath); err == nil && pathSlug == "" {
		meta, haveMeta = m, true
		if m.BaseURL != "" && !baseSet {
			baseURL = strings.TrimRight(m.BaseURL, "/")
		}
	}

	// A remembered proposal ID only means anything on the server it was
	// created against; when --base points somewhere else, start fresh there.
	if meta.ProposalID != 0 && strings.TrimRight(meta.BaseURL, "/") != baseURL {
		meta.ProposalID = 0
	}

	token, tokenSource, err := loadToken()
	if err != nil {
		return err
	}

	reqBody := map[string]string{
		"title": *title, "summary": *summary, "markdown": string(src),
	}
	if pathSlug != "" {
		reqBody["path"] = pathSlug
	} else {
		reqBody["course"], reqBody["language"] = course, language
	}

	// createProposal POSTs a fresh proposal, recovering from the server's
	// 409 duplicate_proposal by locating the existing open proposal (e.g.
	// one opened in the web editor) and updating it instead.
	createProposal := func() (proposalResponse, error) {
		p, err := sendProposal(http.MethodPost, baseURL+"/api/v1/proposals", token, tokenSource, baseURL, mdPath, reqBody)
		if _, isDup := errors.AsType[*duplicateProposalError](err); isDup {
			id, findErr := findMyOpenProposal(baseURL, token, course, language, pathSlug)
			if findErr != nil {
				return proposalResponse{}, fmt.Errorf("%w (and finding the existing proposal failed: %v)", err, findErr)
			}
			return sendProposal(http.MethodPut, fmt.Sprintf("%s/api/v1/proposals/%d", baseURL, id), token, tokenSource, baseURL, mdPath, reqBody)
		}
		return p, err
	}

	var p proposalResponse
	if haveMeta && meta.ProposalID != 0 {
		p, err = sendProposal(http.MethodPut, fmt.Sprintf("%s/api/v1/proposals/%d", baseURL, meta.ProposalID), token, tokenSource, baseURL, mdPath, reqBody)
		// Fall through to creating a fresh proposal only when the remembered
		// one is genuinely unusable — gone, closed, or now targeting a
		// different variant because this file's frontmatter changed (the
		// create still recovers if a different open proposal already holds
		// this variant). A transient failure (network, 5xx, auth) surfaces
		// as-is; retrying it as a POST would just stack a second confusing
		// error on top.
		if proposalGone(err) {
			p, err = createProposal()
		}
	} else {
		p, err = createProposal()
	}
	if err != nil {
		return err
	}

	// Remember the proposal in the sidecar so the next `duck propose` on
	// this file updates it directly. Best-effort: the proposal exists on
	// the server either way. Path documents skip this — no sidecar flow.
	if pathSlug == "" {
		meta.BaseURL, meta.Course, meta.Language, meta.ProposalID = baseURL, course, language, p.ID
		if err := writeEducatorMeta(mdPath, meta); err != nil {
			fmt.Fprintf(os.Stderr, "duck: warning: proposal #%d created, but writing the sidecar failed: %v\n", p.ID, err)
		}
	}

	verb := "proposed"
	if p.Revision > 1 {
		verb = fmt.Sprintf("updated (revision %d — earlier approvals reset)", p.Revision)
	}
	fmt.Printf("%s %s as proposal #%d\nreview it at %s%s\n", verb, target, p.ID, baseURL, p.URL)
	return nil
}

// duplicateProposalError marks the server's 409 duplicate_proposal so
// proposeCmd can recover by updating the existing proposal.
type duplicateProposalError struct{ msg string }

func (e *duplicateProposalError) Error() string { return e.msg }

// apiStatusError preserves the HTTP status and API error code of a failed
// proposal request so callers can branch on what actually went wrong (see
// proposalGone) instead of pattern-matching message text.
type apiStatusError struct {
	status int
	code   string
	msg    string
}

func (e *apiStatusError) Error() string { return e.msg }

// proposalGone reports whether an update failed because the remembered
// proposal can no longer accept this document — it doesn't exist (404), is
// closed, or targets a different course variant than the file's frontmatter
// now names. Those are the only cases where creating a fresh proposal is
// the right recovery.
func proposalGone(err error) bool {
	var apiErr *apiStatusError
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.status == http.StatusNotFound {
		return true
	}
	return apiErr.status == http.StatusConflict &&
		(apiErr.code == "proposal_closed" || apiErr.code == "variant_mismatch")
}

// sendProposal POSTs/PUTs a proposal body and decodes the response,
// translating the API's error shapes into the CLI's messages.
func sendProposal(method, url, token, tokenSource, baseURL, mdPath string, body map[string]string) (proposalResponse, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return proposalResponse{}, err
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(b))
	if err != nil {
		return proposalResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := apiClient.Do(req)
	if err != nil {
		return proposalResponse{}, fmt.Errorf("propose: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return proposalResponse{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return proposalResponse{}, unauthorizedErr(tokenSource, baseURL)
	}

	var errResp struct {
		Error struct {
			Code    string        `json:"code"`
			Message string        `json:"message"`
			Details []lintProblem `json:"details"`
		} `json:"error"`
	}
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
	case http.StatusConflict:
		_ = json.Unmarshal(respBody, &errResp)
		if errResp.Error.Code == "duplicate_proposal" {
			return proposalResponse{}, &duplicateProposalError{msg: "you already have an open proposal for this course variant"}
		}
		if errResp.Error.Message != "" {
			return proposalResponse{}, &apiStatusError{status: resp.StatusCode, code: errResp.Error.Code,
				msg: fmt.Sprintf("propose: %s: %s", resp.Status, errResp.Error.Message)}
		}
		return proposalResponse{}, &apiStatusError{status: resp.StatusCode,
			msg: fmt.Sprintf("propose: server said %s: %s", resp.Status, respBody)}
	case http.StatusUnprocessableEntity:
		if err := json.Unmarshal(respBody, &errResp); err == nil &&
			(errResp.Error.Code == "invalid_course_markdown" || errResp.Error.Code == "invalid_path_markdown") {
			printProblems(mdPath, errResp.Error.Details)
			return proposalResponse{}, fmt.Errorf("%d problem(s) found — fix before proposing", len(errResp.Error.Details))
		}
		return proposalResponse{}, fmt.Errorf("propose: server said %s: %s", resp.Status, respBody)
	default:
		_ = json.Unmarshal(respBody, &errResp)
		return proposalResponse{}, &apiStatusError{status: resp.StatusCode, code: errResp.Error.Code,
			msg: fmt.Sprintf("propose: server said %s: %s", resp.Status, respBody)}
	}

	var p proposalResponse
	if err := json.Unmarshal(respBody, &p); err != nil {
		return proposalResponse{}, fmt.Errorf("parse response: %w", err)
	}
	return p, nil
}

// findMyOpenProposal locates the caller's open proposal for one target — a
// course variant, or a learning path when pathSlug is non-empty — via
// GET /api/v1/proposals (the "mine" list).
func findMyOpenProposal(baseURL, token, course, language, pathSlug string) (int64, error) {
	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/v1/proposals?mine=1", nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := apiClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("list proposals: server said %s", resp.Status)
	}
	var payload struct {
		Proposals []proposalResponse `json:"proposals"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, err
	}
	for _, p := range payload.Proposals {
		if p.Status != "open" {
			continue
		}
		if pathSlug != "" && p.Path == pathSlug {
			return p.ID, nil
		}
		if pathSlug == "" && p.Course == course && p.Language == language {
			return p.ID, nil
		}
	}
	if pathSlug != "" {
		return 0, fmt.Errorf("no open proposal found for path %s", pathSlug)
	}
	return 0, fmt.Errorf("no open proposal found for %s/%s", course, language)
}
