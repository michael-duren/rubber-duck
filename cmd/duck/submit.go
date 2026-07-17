package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/michael-duren/rubber-duck/internal/grader"
)

// submitCmd is local-first: it runs the challenge's tests here, submits the
// code together with the claimed verdict, and prints the result immediately —
// the server re-grades in the background as an audit nobody waits on. The
// slow synchronous server-graded path remains as --remote and as the
// fallback when the language's toolchain isn't installed.
func submitCmd(args []string) error {
	fs := flag.NewFlagSet("submit", flag.ContinueOnError)
	remote := fs.Bool("remote", false, "skip the local test run; grade on the server and wait for it")
	rest, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return usageHelp("submit")
	}
	slug := rest[0]

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, meta, err := findCourseRoot(cwd)
	if err != nil {
		return err
	}
	files, ok := grader.LanguageFiles[meta.Language]
	if !ok {
		return fmt.Errorf("unsupported language %q", meta.Language)
	}
	codePath := filepath.Join(root, slug, files.Code)
	code, err := os.ReadFile(codePath)
	if err != nil {
		return fmt.Errorf("read solution (did you `duck pull` and pick a real slug?): %w", err)
	}

	claim := !*remote
	if claim {
		tool, err := toolchainFor(meta.Language)
		if err != nil {
			return err
		}
		if _, lookErr := exec.LookPath(tool); lookErr != nil {
			fmt.Printf("%s not found locally — grading on the server instead (slower)\n", tool)
			claim = false
		}
	}

	form := url.Values{"code": {string(code)}}
	var local runResult
	if claim {
		local, err = runChallenge(filepath.Join(root, slug), meta.Language)
		if err != nil {
			return err
		}
		status := grader.StatusFailed
		if local.passed {
			status = grader.StatusPassed
		}
		output := local.output
		if local.timedOut {
			output += "\n[time limit exceeded locally]"
		}
		form.Set("claimed_status", status)
		form.Set("claimed_output", output)
	}

	token, tokenSource, err := loadToken()
	if err != nil {
		return err
	}
	base := strings.TrimRight(meta.BaseURL, "/")
	submitURL := fmt.Sprintf("%s/courses/%s/%s/challenges/%s/submissions", base, meta.Course, meta.Language, slug)

	req, err := http.NewRequest("POST", submitURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("submit: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized {
		return unauthorizedErr(tokenSource, base)
	}
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("submit: server said %s: %s", resp.Status, body)
	}
	var created struct {
		ID     int64  `json:"id"`
		URL    string `json:"url"`
		Status string `json:"status"`
		Score  int    `json:"score"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		return fmt.Errorf("parse submit response: %w", err)
	}
	fmt.Printf("submission: %s%s\n", base, created.URL)

	if !claim {
		return pollSubmission(base, created.URL, token)
	}

	if created.Status == grader.StatusPassed {
		fmt.Printf("passed — +%d pts (server audit runs in the background)\n", created.Score)
		return nil
	}
	fmt.Println(local.output)
	fmt.Println("failed")
	return fmt.Errorf("tests failed")
}

func pollSubmission(base, path, token string) error {
	statusURL := base + path + "/status"
	for {
		req, err := http.NewRequest("GET", statusURL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("poll status: %w", err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("poll status: server said %s: %s", resp.Status, body)
		}
		var status struct {
			Status string `json:"status"`
			Score  int    `json:"score"`
			Output string `json:"output"`
		}
		if err := json.Unmarshal(body, &status); err != nil {
			return fmt.Errorf("parse status response: %w", err)
		}

		switch status.Status {
		case "passed":
			fmt.Printf("passed — +%d pts\n", status.Score)
			return nil
		case "failed":
			fmt.Println("failed")
			fmt.Println(status.Output)
			return fmt.Errorf("tests failed")
		case "error":
			fmt.Println(status.Output)
			return fmt.Errorf("grading error")
		default:
			time.Sleep(1500 * time.Millisecond)
		}
	}
}
