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
		return fmt.Errorf("usage: duck propose [file] [--title T] [--summary S]")
	}
	mdPath, err := resolveEducatorFile(rest)
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
		return fmt.Errorf("%d problem(s) found — fix before proposing", len(probs))
	}
	res, err := ingest.Parse(src)
	if err != nil {
		return fmt.Errorf("%s: %w", mdPath, err)
	}
	course, language := res.Course.Course, res.Course.Language

	// A sidecar is optional here, but when present it pins the server and
	// remembers the open proposal this file previously created.
	baseURL := strings.TrimRight(*base, "/")
	var meta educatorMeta
	haveMeta := false
	if m, err := readEducatorMeta(mdPath); err == nil {
		meta, haveMeta = m, true
		if m.BaseURL != "" {
			baseURL = strings.TrimRight(m.BaseURL, "/")
		}
	}

	token, tokenSource, err := loadToken()
	if err != nil {
		return err
	}

	reqBody := map[string]string{
		"course": course, "language": language,
		"title": *title, "summary": *summary, "markdown": string(src),
	}

	var p proposalResponse
	if haveMeta && meta.ProposalID != 0 {
		p, err = sendProposal(http.MethodPut, fmt.Sprintf("%s/api/v1/proposals/%d", baseURL, meta.ProposalID), token, tokenSource, baseURL, mdPath, reqBody)
		// A 404/409 here means that remembered proposal is gone or closed —
		// fall through to creating a fresh one rather than dead-ending.
		if err != nil {
			p, err = sendProposal(http.MethodPost, baseURL+"/api/v1/proposals", token, tokenSource, baseURL, mdPath, reqBody)
		}
	} else {
		p, err = sendProposal(http.MethodPost, baseURL+"/api/v1/proposals", token, tokenSource, baseURL, mdPath, reqBody)
		var dup *duplicateProposalError
		if errors.As(err, &dup) {
			// The server knows we already have an open proposal for this
			// variant; find it and update it instead.
			id, findErr := findMyOpenProposal(baseURL, token, course, language)
			if findErr != nil {
				return fmt.Errorf("%w (and finding the existing proposal failed: %v)", err, findErr)
			}
			p, err = sendProposal(http.MethodPut, fmt.Sprintf("%s/api/v1/proposals/%d", baseURL, id), token, tokenSource, baseURL, mdPath, reqBody)
		}
	}
	if err != nil {
		return err
	}

	// Remember the proposal in the sidecar so the next `duck propose` on
	// this file updates it directly. Best-effort: the proposal exists on
	// the server either way.
	meta.BaseURL, meta.Course, meta.Language, meta.ProposalID = baseURL, course, language, p.ID
	if err := writeEducatorMeta(mdPath, meta); err != nil {
		fmt.Fprintf(os.Stderr, "duck: warning: proposal #%d created, but writing the sidecar failed: %v\n", p.ID, err)
	}

	verb := "proposed"
	if p.Revision > 1 {
		verb = fmt.Sprintf("updated (revision %d — earlier approvals reset)", p.Revision)
	}
	fmt.Printf("%s %s/%s as proposal #%d\nreview it at %s%s\n", verb, course, language, p.ID, baseURL, p.URL)
	return nil
}

// duplicateProposalError marks the server's 409 duplicate_proposal so
// proposeCmd can recover by updating the existing proposal.
type duplicateProposalError struct{ msg string }

func (e *duplicateProposalError) Error() string { return e.msg }

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
			return proposalResponse{}, fmt.Errorf("propose: %s: %s", resp.Status, errResp.Error.Message)
		}
		return proposalResponse{}, fmt.Errorf("propose: server said %s: %s", resp.Status, respBody)
	case http.StatusUnprocessableEntity:
		if err := json.Unmarshal(respBody, &errResp); err == nil && errResp.Error.Code == "invalid_course_markdown" {
			printProblems(mdPath, errResp.Error.Details)
			return proposalResponse{}, fmt.Errorf("%d problem(s) found — fix before proposing", len(errResp.Error.Details))
		}
		return proposalResponse{}, fmt.Errorf("propose: server said %s: %s", resp.Status, respBody)
	default:
		return proposalResponse{}, fmt.Errorf("propose: server said %s: %s", resp.Status, respBody)
	}

	var p proposalResponse
	if err := json.Unmarshal(respBody, &p); err != nil {
		return proposalResponse{}, fmt.Errorf("parse response: %w", err)
	}
	return p, nil
}

// findMyOpenProposal locates the caller's open proposal for one course
// variant via GET /api/v1/proposals (the "mine" list).
func findMyOpenProposal(baseURL, token, course, language string) (int64, error) {
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
		if p.Course == course && p.Language == language && p.Status == "open" {
			return p.ID, nil
		}
	}
	return 0, fmt.Errorf("no open proposal found for %s/%s", course, language)
}
