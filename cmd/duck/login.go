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

// sessionCookieName mirrors internal/web's gc_session cookie: its presence
// after POSTing /login is how we tell a real login success (which sets it)
// apart from the server re-rendering the login page with a "wrong
// username or password" message (which also answers 200 OK).
const sessionCookieName = "gc_session"

// loginStdin and loginReadPassword make the credential prompt swappable in
// tests: term.ReadPassword needs a real terminal fd, so tests substitute a
// fake reader instead of driving a pty.
var (
	loginStdin       io.Reader = os.Stdin
	loginReadPassword           = func() ([]byte, error) { return term.ReadPassword(int(os.Stdin.Fd())) }
)

func loginCmd(args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // suppress default help output
	baseURL := fs.String("base", "https://gc-app-aauuwonajq-uc.a.run.app", "server base URL")
	if rest, err := parseInterleaved(fs, args); err != nil || len(rest) != 0 {
		return fmt.Errorf("usage: duck login [--base URL]")
	}

	fmt.Print("username: ")
	var username string
	if _, err := fmt.Fscanln(loginStdin, &username); err != nil {
		return fmt.Errorf("read username: %w", err)
	}
	fmt.Print("password: ")
	password, err := loginReadPassword()
	if err != nil {
		return fmt.Errorf("read password: %w", err)
	}
	fmt.Println() // newline after password prompt

	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	client := &http.Client{Jar: jar}

	token, err := fetchCLIToken(client, *baseURL, username, string(password))
	if err != nil {
		return err
	}

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

// fetchCLIToken drives the browser-equivalent login + token-mint flow with
// client (which must carry a cookie jar). The server's CSRF check is a
// double-submit cookie: every unsafe POST needs a token lifted from a GET
// of the same page first, so login and token-minting are each a GET-then-POST
// pair rather than a bare POST.
func fetchCLIToken(client *http.Client, base, username, password string) (string, error) {
	base = strings.TrimRight(base, "/")

	loginPage, err := getBody(client, base+"/login")
	if err != nil {
		return "", fmt.Errorf("fetch login page: %w", err)
	}
	csrfToken := extractCSRFToken(loginPage)
	if csrfToken == "" {
		return "", fmt.Errorf("couldn't find CSRF token on login page")
	}

	loginReq, err := http.NewRequest("POST", base+"/login", strings.NewReader(
		url.Values{"username": {username}, "password": {password}, "csrf_token": {csrfToken}}.Encode(),
	))
	if err != nil {
		return "", err
	}
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginResp, err := client.Do(loginReq)
	if err != nil {
		return "", fmt.Errorf("login: %w", err)
	}
	defer func() { _ = loginResp.Body.Close() }()
	_, _ = io.ReadAll(loginResp.Body)

	if !hasCookie(client, base, sessionCookieName) {
		return "", fmt.Errorf("login failed: check your username and password")
	}

	profilePage, err := getBody(client, base+"/profile")
	if err != nil {
		return "", fmt.Errorf("fetch profile: %w", err)
	}
	csrfToken = extractCSRFToken(profilePage)
	if csrfToken == "" {
		return "", fmt.Errorf("couldn't find CSRF token in profile page")
	}

	tokenReq, err := http.NewRequest("POST", base+"/profile/tokens", strings.NewReader(
		url.Values{"name": {"duck login"}, "csrf_token": {csrfToken}}.Encode(),
	))
	if err != nil {
		return "", err
	}
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenResp, err := client.Do(tokenReq)
	if err != nil {
		return "", fmt.Errorf("mint token: %w", err)
	}
	defer func() { _ = tokenResp.Body.Close() }()

	tokenBody, err := io.ReadAll(tokenResp.Body)
	if err != nil {
		return "", fmt.Errorf("read mint-token response: %w", err)
	}
	if tokenResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("mint token failed: server said %s", tokenResp.Status)
	}

	token := extractToken(string(tokenBody))
	if token == "" {
		return "", fmt.Errorf("couldn't find token in response")
	}
	return token, nil
}

// getBody GETs url with client and returns the response body as a string,
// erroring on a non-200 status.
func getBody(client *http.Client, url string) (string, error) {
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server said %s", resp.Status)
	}
	return string(body), nil
}

// hasCookie reports whether client's jar holds a cookie named name for base.
func hasCookie(client *http.Client, base, name string) bool {
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	for _, c := range client.Jar.Cookies(u) {
		if c.Name == name {
			return true
		}
	}
	return false
}

// extractCSRFToken parses the CSRF token from a page's HTML.
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
