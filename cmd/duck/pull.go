package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/michael-duren/rubber-duck/internal/grader"
)

type challengeJSON struct {
	LessonSlug   string `json:"lesson_slug"`
	LessonNumber int    `json:"lesson_number"` // 1-based lesson position; 0 for the final
	Slug         string `json:"slug"`
	Title        string `json:"title"`
	Points       int    `json:"points"`
	StarterCode  string `json:"starter_code"`
	TestCode     string `json:"test_code"`
}

func pullCmd(args []string) error {
	fs := flag.NewFlagSet("pull", flag.ContinueOnError)
	base := fs.String("base", envOr("DUCK_BASE_URL", "https://duckgc.com"), "server base URL")
	rest, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return usageHelp("pull")
	}
	course, language, ok := strings.Cut(rest[0], "/")
	if !ok || course == "" || language == "" {
		return usageHelp("pull")
	}
	files, ok := grader.LanguageFiles[language]
	if !ok {
		return fmt.Errorf("unsupported language %q", language)
	}

	listURL := strings.TrimRight(*base, "/") + "/api/v1/courses/" + url.PathEscape(course) + "/variants/" + url.PathEscape(language) + "/challenges"
	resp, err := apiClient.Get(listURL)
	if err != nil {
		return fmt.Errorf("fetch challenges: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
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

	dirNames := challengeDirNames(listing.Challenges)
	for i, c := range listing.Challenges {
		// The slug names a local directory, and it came off the network:
		// never let a malicious or broken server walk it out of root
		// (e.g. a slug of "../../.bashrc").
		if c.Slug == "" || c.Slug == "." || c.Slug == ".." || c.Slug != filepath.Base(c.Slug) {
			return fmt.Errorf("server returned unsafe challenge slug %q", c.Slug)
		}
		dir := filepath.Join(root, dirNames[i])
		if _, err := os.Stat(dir); err == nil {
			fmt.Printf("skip %s (already exists)\n", dir)
			continue
		}
		// A pre-ordering pull scaffolded this challenge under the bare
		// slug; leave that work in progress alone rather than pulling a
		// duplicate copy under the new name.
		if _, err := os.Stat(filepath.Join(root, c.Slug)); err == nil {
			fmt.Printf("skip %s (already exists as %s)\n", dir, filepath.Join(root, c.Slug))
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
