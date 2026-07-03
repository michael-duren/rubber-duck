package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const courseMetaFile = ".duck-course.json"

// courseMeta is written by `duck pull` at the root of the scaffolded course
// directory so `duck test`/`duck submit` can find the server and course/language
// without the user repeating them.
type courseMeta struct {
	BaseURL  string `json:"base_url"`
	Course   string `json:"course"`
	Language string `json:"language"`
}

func writeCourseMeta(dir string, m courseMeta) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, courseMetaFile), b, 0o644)
}

// findCourseRoot walks up from start looking for a directory `duck pull`
// scaffolded, so commands can be run from anywhere inside the course tree.
func findCourseRoot(start string) (root string, meta courseMeta, err error) {
	dir := start
	for {
		b, err := os.ReadFile(filepath.Join(dir, courseMetaFile))
		if err == nil {
			var m courseMeta
			if err := json.Unmarshal(b, &m); err != nil {
				return "", courseMeta{}, fmt.Errorf("parse %s: %w", courseMetaFile, err)
			}
			return dir, m, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", courseMeta{}, fmt.Errorf("not inside a course pulled with `duck pull` (no %s found)", courseMetaFile)
		}
		dir = parent
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// loadToken resolves the user's CLI token: DUCK_TOKEN env var first, then
// ~/.config/duck/token.
func loadToken() (string, error) {
	if t := os.Getenv("DUCK_TOKEN"); t != "" {
		return t, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("no DUCK_TOKEN set and can't find home dir: %w", err)
	}
	b, err := os.ReadFile(filepath.Join(home, ".config", "duck", "token"))
	if err != nil {
		return "", fmt.Errorf("no DUCK_TOKEN set and no token file (%s): mint one on the profile page and set DUCK_TOKEN, or save it to ~/.config/duck/token", filepath.Join(home, ".config", "duck", "token"))
	}
	return strings.TrimSpace(string(b)), nil
}
