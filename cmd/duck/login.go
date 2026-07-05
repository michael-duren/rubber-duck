package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"
)

func loginCmd(args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // suppress default help output
	baseURL := fs.String("base", "https://gc-app-aauuwonajq-uc.a.run.app", "server base URL")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: duck login [--base URL]")
	}

	*baseURL = strings.TrimRight(*baseURL, "/")

	// Prompt for credentials
	fmt.Print("username: ")
	var username string
	if _, err := fmt.Scanln(&username); err != nil {
		return fmt.Errorf("read username: %w", err)
	}
	fmt.Print("password: ")
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("read password: %w", err)
	}
	fmt.Println() // newline after password prompt

	// Create HTTP client with cookie jar
	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	client := &http.Client{Jar: jar}

	// POST to /login
	loginReq, err := http.NewRequest("POST", *baseURL+"/login", strings.NewReader(
		url.Values{"username": {username}, "password": {string(password)}}.Encode(),
	))
	if err != nil {
		return err
	}
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginResp, err := client.Do(loginReq)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	defer func() { _ = loginResp.Body.Close() }()
	_, _ = io.ReadAll(loginResp.Body)

	if loginResp.StatusCode != http.StatusOK && loginResp.StatusCode != http.StatusSeeOther {
		return fmt.Errorf("login failed: server said %s", loginResp.Status)
	}

	// GET /profile to fetch CSRF token from HTML
	profileReq, err := http.NewRequest("GET", *baseURL+"/profile", nil)
	if err != nil {
		return err
	}
	profileResp, err := client.Do(profileReq)
	if err != nil {
		return fmt.Errorf("fetch profile: %w", err)
	}
	defer func() { _ = profileResp.Body.Close() }()

	if profileResp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch profile failed: server said %s", profileResp.Status)
	}

	profileBody, _ := io.ReadAll(profileResp.Body)
	csrfToken := extractCSRFToken(string(profileBody))
	if csrfToken == "" {
		return fmt.Errorf("couldn't find CSRF token in profile page")
	}

	// POST to /profile/tokens to mint a token
	tokenReq, err := http.NewRequest("POST", *baseURL+"/profile/tokens", strings.NewReader(
		url.Values{"name": {"duck login"}, "csrf_token": {csrfToken}}.Encode(),
	))
	if err != nil {
		return err
	}
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenResp, err := client.Do(tokenReq)
	if err != nil {
		return fmt.Errorf("mint token: %w", err)
	}
	defer func() { _ = tokenResp.Body.Close() }()

	tokenBody, _ := io.ReadAll(tokenResp.Body)
	if tokenResp.StatusCode != http.StatusOK {
		return fmt.Errorf("mint token failed: server said %s", tokenResp.Status)
	}

	// Extract the minted token from the response HTML
	token := extractToken(string(tokenBody))
	if token == "" {
		return fmt.Errorf("couldn't find token in response")
	}

	// Save token to ~/.config/duck/token
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("find home dir: %w", err)
	}
	tokenDir := filepath.Join(home, ".config", "duck")
	if err := os.MkdirAll(tokenDir, 0o755); err != nil {
		return fmt.Errorf("create token directory: %w", err)
	}

	tokenPath := filepath.Join(tokenDir, "token")
	if err := os.WriteFile(tokenPath, []byte(token), 0o600); err != nil {
		return fmt.Errorf("write token: %w", err)
	}

	fmt.Printf("token saved to %s\n", tokenPath)
	return nil
}

// extractCSRFToken parses the CSRF token from the profile page HTML.
// Looks for an input with name="csrf_token" and extracts its value.
func extractCSRFToken(html string) string {
	// Simple extraction: look for name="csrf_token" value="..."
	start := strings.Index(html, `name="csrf_token"`)
	if start == -1 {
		return ""
	}
	// Look for value=" after the name attribute
	valueIdx := strings.Index(html[start:], `value="`)
	if valueIdx == -1 {
		return ""
	}
	valueStart := start + valueIdx + len(`value="`)
	valueEnd := strings.Index(html[valueStart:], `"`)
	if valueEnd == -1 {
		return ""
	}
	return html[valueStart : valueStart+valueEnd]
}

// extractToken parses the minted token from the profile page HTML.
// Looks for the token in a <code> tag that appears after "Copy this token now".
func extractToken(html string) string {
	// Look for the "Copy this token now" message followed by the code tag
	marker := "Copy this token now"
	idx := strings.Index(html, marker)
	if idx == -1 {
		return ""
	}
	// Look for <code> tag after the marker
	codeStart := strings.Index(html[idx:], "<code")
	if codeStart == -1 {
		return ""
	}
	codeStart += idx
	// Find the closing > of the code tag
	tagEnd := strings.Index(html[codeStart:], ">")
	if tagEnd == -1 {
		return ""
	}
	contentStart := codeStart + tagEnd + 1
	// Find the closing </code> tag
	contentEnd := strings.Index(html[contentStart:], "</code>")
	if contentEnd == -1 {
		return ""
	}
	return strings.TrimSpace(html[contentStart : contentStart+contentEnd])
}
