package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mduren/getcracked/internal/grader"
)

func submitCmd(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: duck submit <challenge-slug>")
	}
	slug := args[0]

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

	token, err := loadToken()
	if err != nil {
		return err
	}
	base := strings.TrimRight(meta.BaseURL, "/")
	submitURL := fmt.Sprintf("%s/courses/%s/%s/challenges/%s/submissions", base, meta.Course, meta.Language, slug)

	req, err := http.NewRequest("POST", submitURL, strings.NewReader(url.Values{"code": {string(code)}}.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("submit: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("unauthorized: token missing or revoked")
	}
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("submit: server said %s: %s", resp.Status, body)
	}
	var created struct {
		ID  int64  `json:"id"`
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		return fmt.Errorf("parse submit response: %w", err)
	}
	fmt.Printf("submission: %s%s\n", base, created.URL)

	return pollSubmission(base, created.URL, token)
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
		resp.Body.Close()
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
