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

// tokenSourceEnv/tokenSourceFile name where loadToken found the token, for
// error messages and `duck auth status` — "your token was rejected" is only
// actionable if the user knows WHICH token was sent (a stale DUCK_TOKEN
// silently shadows the file a fresh `duck auth login` just wrote).
const (
	tokenSourceEnv  = "the DUCK_TOKEN environment variable"
	tokenSourceFile = "~/.config/duck/token"
)

// tokenFilePath returns the CLI token file path (~/.config/duck/token).
func tokenFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "duck", "token"), nil
}

// loadToken resolves the user's CLI token: DUCK_TOKEN env var first, then
// ~/.config/duck/token. source is one of the tokenSource* constants.
func loadToken() (token, source string, err error) {
	if t := os.Getenv("DUCK_TOKEN"); t != "" {
		return t, tokenSourceEnv, nil
	}
	path, err := tokenFilePath()
	if err != nil {
		return "", "", fmt.Errorf("no DUCK_TOKEN set and can't find home dir: %w", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("no DUCK_TOKEN set and no token file (%s): run `duck auth login`, or mint a token on the profile page and set DUCK_TOKEN or save it to ~/.config/duck/token", path)
	}
	return strings.TrimSpace(string(b)), tokenSourceFile, nil
}

// unauthorizedErr renders a 401 from the server actionably: it names the
// token's origin and where it was sent, because "token missing or revoked"
// alone sends people in circles when a stale DUCK_TOKEN is shadowing a
// freshly saved token file.
func unauthorizedErr(source, base string) error {
	return fmt.Errorf("unauthorized: the token from %s was rejected by %s — run `duck auth status` to diagnose, `duck auth login` to fix", source, base)
}
