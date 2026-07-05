package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/michael-duren/rubber-duck/internal/grader"
)

type challengeJSON struct {
	LessonSlug  string `json:"lesson_slug"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Points      int    `json:"points"`
	StarterCode string `json:"starter_code"`
	TestCode    string `json:"test_code"`
}

func pullCmd(args []string) error {
	fs := flag.NewFlagSet("pull", flag.ContinueOnError)
	base := fs.String("base", envOr("DUCK_BASE_URL", "http://localhost:8080"), "server base URL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return fmt.Errorf("usage: duck pull <course>/<language>")
	}
	course, language, ok := strings.Cut(rest[0], "/")
	if !ok || course == "" || language == "" {
		return fmt.Errorf("usage: duck pull <course>/<language>")
	}
	files, ok := grader.LanguageFiles[language]
	if !ok {
		return fmt.Errorf("unsupported language %q", language)
	}

	url := strings.TrimRight(*base, "/") + "/api/v1/courses/" + course + "/variants/" + language + "/challenges"
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("fetch challenges: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch challenges: server said %s: %s", resp.Status, body)
	}
	var listing struct {
		Challenges []challengeJSON `json:"challenges"`
	}
	if err := json.Unmarshal(body, &listing); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	root := course + "-" + language
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	if err := writeCourseMeta(root, courseMeta{BaseURL: *base, Course: course, Language: language}); err != nil {
		return fmt.Errorf("write %s: %w", courseMetaFile, err)
	}

	for _, c := range listing.Challenges {
		dir := filepath.Join(root, c.Slug)
		if _, err := os.Stat(dir); err == nil {
			fmt.Printf("skip %s (already exists)\n", dir)
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, files.Code), []byte(c.StarterCode), 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, files.Tests), []byte(c.TestCode), 0o644); err != nil {
			return err
		}
		if language == "go" {
			mod := "module challenge\n\ngo 1.21\n"
			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644); err != nil {
				return err
			}
		}
		fmt.Printf("pulled %s (%d pts)\n", dir, c.Points)
	}
	return nil
}
