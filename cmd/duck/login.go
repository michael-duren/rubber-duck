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
	"time"

	"golang.org/x/term"
)

// sessionCookieName mirrors internal/web's gc_session cookie (sessionCookie
// in internal/web/session.go): its presence after POSTing /login is how we
// tell a real login success apart from the server re-rendering the login
// page with a "wrong username or password" message — both look like 200 OK
// to this client once it follows the success path's 303 redirect.
const sessionCookieName = "gc_session"

// loginStdin and loginReadPassword make the credential prompt swappable in
// tests: term.ReadPassword needs a real terminal fd, so tests substitute a
// fake reader instead of driving a pty.
var (
	loginStdin        io.Reader = os.Stdin
	loginReadPassword           = func() ([]byte, error) { return term.ReadPassword(int(os.Stdin.Fd())) }
)

func loginCmd(args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // suppress default help output
	baseURL := fs.String("base", "https://gc-app-aauuwonajq-uc.a.run.app", "server base URL")
	rest, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
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
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}

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
// double-submit cookie: every unsafe POST needs a token lifted from a
// previously GET-rendered page, so login and token-minting are each a
// GET-then-POST pair rather than a bare POST.
func fetchCLIToken(client *http.Client, base, username, password string) (string, error) {
	base = strings.TrimRight(base, "/")
	baseU, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("invalid base URL %q: %w", base, err)
	}

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
	loginBody, err := io.ReadAll(loginResp.Body)
	if err != nil {
		return "", fmt.Errorf("read login response: %w", err)
	}
	if loginResp.StatusCode != http.StatusOK {
		// A 403 here means the CSRF dance broke, a 5xx means the server
		// did — neither is the user's fault, so surface the server's own
		// words instead of guessing at credentials.
		if s := firstLine(string(loginBody)); s != "" {
			return "", fmt.Errorf("login failed: server said %s: %s", loginResp.Status, s)
		}
		return "", fmt.Errorf("login failed: server said %s", loginResp.Status)
	}

	// On success the client has followed the 303 redirect, so check the jar
	// against the URL we actually landed on — a redirect to another host
	// stores the cookie under that host, not under base.
	finalURL := loginResp.Request.URL
	if !hasCookie(client, finalURL, sessionCookieName) {
		if finalURL.Host != baseU.Host {
			return "", fmt.Errorf("login redirected to %s — check your --base URL", finalURL.Host)
		}
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
		// The mint POST answered 200, so the server has already created and
		// stored a token — we just failed to scrape it out of the page.
		return "", fmt.Errorf("mint succeeded but couldn't find token in the response — a token may have been created on your account (check %s/profile); the page format may have changed, try updating duck", base)
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

// hasCookie reports whether client's jar holds a cookie named name for u.
// Note the jar never returns Secure cookies for an http:// URL, so an http
// base fronted by an https-terminating proxy won't see its session cookie.
func hasCookie(client *http.Client, u *url.URL, name string) bool {
	for _, c := range client.Jar.Cookies(u) {
		if c.Name == name {
			return true
		}
	}
	return false
}

// firstLine returns the first non-empty line of s, trimmed and truncated,
// for embedding a server response body in an error message.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	const max = 120
	if len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

// extractCSRFToken parses the CSRF token from a page's HTML.
// Looks for an input with name="csrf_token" and extracts its value.
func extractCSRFToken(html string) string {
	// Simple extraction: look for name="csrf_token" value="..."
	start := strings.Index(html, `name="csrf_token"`)
	if start == -1 {
		return ""
	}
	// Bound the value=" search to this tag, so that a csrf input with no
	// value attribute can't silently pick up a value= from a later tag.
	tag := html[start:]
	if end := strings.Index(tag, ">"); end != -1 {
		tag = tag[:end]
	}
	valueIdx := strings.Index(tag, `value="`)
	if valueIdx == -1 {
		return ""
	}
	valueStart := valueIdx + len(`value="`)
	valueEnd := strings.Index(tag[valueStart:], `"`)
	if valueEnd == -1 {
		return ""
	}
	return tag[valueStart : valueStart+valueEnd]
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
