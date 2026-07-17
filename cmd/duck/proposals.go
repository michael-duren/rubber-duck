package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// proposalsCmd lists the caller's proposals (`duck proposals`) or shows one
// (`duck proposals status <id>`). Reviewing/approving is a web flow — the
// CLI only authors and tracks.
func proposalsCmd(args []string) error {
	if len(args) > 0 && isHelpArg(args[0]) {
		return helpCmd([]string{"proposals"})
	}
	if len(args) > 0 && args[0] == "status" {
		return proposalStatusCmd(args[1:])
	}

	fs := flag.NewFlagSet("proposals", flag.ContinueOnError)
	base := fs.String("base", envOr("DUCK_BASE_URL", "https://duckgc.com"), "server base URL")
	rest, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return fmt.Errorf("usage: duck proposals [status <id>] [--base URL]")
	}

	baseURL := strings.TrimRight(*base, "/")
	token, tokenSource, err := loadToken()
	if err != nil {
		return err
	}

	body, err := getProposalsJSON(baseURL+"/api/v1/proposals?mine=1", token, tokenSource, baseURL)
	if err != nil {
		return err
	}
	var payload struct {
		Proposals []proposalResponse `json:"proposals"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	if len(payload.Proposals) == 0 {
		fmt.Println("no proposals yet — edit a course on the web or run `duck propose` to open one")
		return nil
	}
	for _, p := range payload.Proposals {
		printProposal(baseURL, p)
	}
	return nil
}

func proposalStatusCmd(args []string) error {
	fs := flag.NewFlagSet("proposals status", flag.ContinueOnError)
	base := fs.String("base", envOr("DUCK_BASE_URL", "https://duckgc.com"), "server base URL")
	rest, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return fmt.Errorf("usage: duck proposals status <id>")
	}
	id, err := strconv.ParseInt(rest[0], 10, 64)
	if err != nil {
		return fmt.Errorf("proposal id must be a number, got %q", rest[0])
	}

	baseURL := strings.TrimRight(*base, "/")
	token, tokenSource, err := loadToken()
	if err != nil {
		return err
	}

	body, err := getProposalsJSON(fmt.Sprintf("%s/api/v1/proposals/%d", baseURL, id), token, tokenSource, baseURL)
	if err != nil {
		return err
	}
	var p proposalResponse
	if err := json.Unmarshal(body, &p); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	printProposal(baseURL, p)
	return nil
}

func getProposalsJSON(url, token, tokenSource, baseURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := apiClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch proposals: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, unauthorizedErr(tokenSource, baseURL)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no such proposal")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch proposals: server said %s: %s", resp.Status, body)
	}
	return body, nil
}

func printProposal(baseURL string, p proposalResponse) {
	extra := ""
	if p.Status == "open" {
		extra = fmt.Sprintf("  %d approval(s)", p.Approvals)
		if p.Stale {
			extra += "  NEEDS REBASE (course changed underneath — re-pull, reapply, `duck propose`)"
		}
	}
	fmt.Printf("#%-4d %-9s %s/%s  %q%s\n      %s%s\n",
		p.ID, p.Status, p.Course, p.Language, p.Title, extra, baseURL, p.URL)
}
