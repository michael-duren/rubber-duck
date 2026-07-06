package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// --- extractCSRFToken / extractToken: pure parsing edge cases ---

func TestExtractCSRFToken(t *testing.T) {
	cases := []struct {
		name string
		html string
		want string
	}{
		{
			"normal hidden input",
			`<form><input type="hidden" name="csrf_token" value="abc123"/></form>`,
			"abc123",
		},
		{
			"real layout.templ shape",
			`<form method="post" action="/login" class="mt-6 flex flex-col gap-4">` +
				`<input type="hidden" name="csrf_token" value="30567a23ee9d6fab51d6ebb894b8539e6f2186d4"/>` +
				`<label>Username</label></form>`,
			"30567a23ee9d6fab51d6ebb894b8539e6f2186d4",
		},
		{"no csrf field at all", `<form><input name="username"/></form>`, ""},
		{"name present but no value attr", `<input name="csrf_token" data-x="1"/>`, ""},
		{
			// The value= search must stay inside the csrf input's own tag:
			// grabbing a later tag's value would submit garbage as the CSRF
			// token and surface as a baffling 403 instead of a clear error.
			"no value attr, later tag has one",
			`<input name="csrf_token"/><input name="next" value="/dashboard"/>`,
			"",
		},
		{"empty value", `<input name="csrf_token" value=""/>`, ""},
		{"unterminated value", `<input name="csrf_token" value="abc`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractCSRFToken(c.html); got != c.want {
				t.Errorf("extractCSRFToken(%q) = %q, want %q", c.html, got, c.want)
			}
		})
	}
}

func TestExtractToken(t *testing.T) {
	cases := []struct {
		name string
		html string
		want string
	}{
		{
			"normal",
			`<p>Copy this token now — it won't be shown again.</p>` +
				`<code class="mt-1 block break-all font-mono">gc_u_abc123</code>`,
			"gc_u_abc123",
		},
		{
			"other code tags appear before the marker (real profile page shape)",
			`<pre><code>go install ./cmd/duck</code></pre>` +
				`<p class="font-medium">Copy this token now — it won't be shown again.</p>` +
				`<code class="mt-1 block break-all font-mono text-emerald-900">gc_u_realtoken456</code>`,
			"gc_u_realtoken456",
		},
		{
			"whitespace around token is trimmed",
			`Copy this token now<code>
				gc_u_spacedout
			</code>`,
			"gc_u_spacedout",
		},
		{"marker missing entirely", `<code>gc_u_nope</code>`, ""},
		{"marker present, no code tag after it", `Copy this token now <p>gone</p>`, ""},
		{"marker present, code tag never closed", `Copy this token now <code>gc_u_broken`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractToken(c.html); got != c.want {
				t.Errorf("extractToken(%q) = %q, want %q", c.html, got, c.want)
			}
		})
	}
}

// --- fakeAuthServer: a minimal stand-in for the real login + CLI-token
// flow that enforces the same double-submit-cookie CSRF contract as
// internal/web/csrf.go. Because it actually rejects a POST whose
// csrf_token doesn't match a cookie set by a prior GET, any regression
// that removes the GET-before-POST sequencing in fetchCLIToken will make
// these tests fail with a 403, exactly as it does against the real server.

const fakeCSRFCookie = "gc_csrf"

type csrfCtxKey struct{}

type fakeAuthServer struct {
	mu       sync.Mutex
	username string
	password string
	loggedIn map[string]bool
	nextTok  int
	requests []string

	brokenProfile   bool
	omitLoginCSRF   bool
	loginFails      bool
	omitProfileCSRF bool
	mintFails       bool
	omitMintedToken bool
}

func newFakeAuthServer(username, password string) *fakeAuthServer {
	return &fakeAuthServer{username: username, password: password, loggedIn: map[string]bool{}}
}

func (f *fakeAuthServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", f.getLogin)
	mux.HandleFunc("POST /login", f.postLogin)
	mux.HandleFunc("GET /profile", f.getProfile)
	mux.HandleFunc("POST /profile/tokens", f.postToken)
	// The real server serves the course list at / — the target of the 303
	// after a successful login. Serve a 200 so the login POST's final
	// (post-redirect) status matches production.
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `<html><body>courses</body></html>`)
	})
	srv := httptest.NewServer(f.withCSRF(mux))
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeAuthServer) recordedRequests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

func (f *fakeAuthServer) withCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		f.mu.Unlock()

		token := ""
		if c, err := r.Cookie(fakeCSRFCookie); err == nil {
			token = c.Value
		}
		if token == "" {
			b := make([]byte, 16)
			_, _ = rand.Read(b)
			token = hex.EncodeToString(b)
			http.SetCookie(w, &http.Cookie{Name: fakeCSRFCookie, Value: token, Path: "/", HttpOnly: true})
		}
		r = r.WithContext(context.WithValue(r.Context(), csrfCtxKey{}, token))

		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				http.Error(w, "bad form", http.StatusBadRequest)
				return
			}
			if r.FormValue("csrf_token") != token {
				http.Error(w, "invalid or missing csrf token", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func csrfFromCtx(r *http.Request) string {
	t, _ := r.Context().Value(csrfCtxKey{}).(string)
	return t
}

func (f *fakeAuthServer) hasSession(r *http.Request) bool {
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loggedIn[c.Value]
}

func (f *fakeAuthServer) getLogin(w http.ResponseWriter, r *http.Request) {
	if f.omitLoginCSRF {
		_, _ = fmt.Fprint(w, `<html><body><form method="post" action="/login"></form></body></html>`)
		return
	}
	_, _ = fmt.Fprintf(w, `<html><body><form method="post" action="/login">`+
		`<input type="hidden" name="csrf_token" value="%s"/>`+
		`</form></body></html>`, csrfFromCtx(r))
}

func (f *fakeAuthServer) postLogin(w http.ResponseWriter, r *http.Request) {
	if f.loginFails {
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	if r.FormValue("username") != f.username || r.FormValue("password") != f.password {
		// Mirrors the real handler: wrong credentials re-render the login
		// page with 200 OK, not an error status — the only reliable signal
		// of failure is that no session cookie gets set.
		_, _ = fmt.Fprint(w, `<html><body>Wrong username or password.</body></html>`)
		return
	}
	f.mu.Lock()
	f.nextTok++
	sess := fmt.Sprintf("sess-%d", f.nextTok)
	f.loggedIn[sess] = true
	f.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: sess, Path: "/", HttpOnly: true})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (f *fakeAuthServer) getProfile(w http.ResponseWriter, r *http.Request) {
	if f.brokenProfile {
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	if !f.hasSession(r) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if f.omitProfileCSRF {
		_, _ = fmt.Fprint(w, `<html><body>no csrf field here</body></html>`)
		return
	}
	_, _ = fmt.Fprintf(w, `<html><body><form method="post" action="/profile/tokens">`+
		`<input type="hidden" name="csrf_token" value="%s"/>`+
		`</form></body></html>`, csrfFromCtx(r))
}

func (f *fakeAuthServer) postToken(w http.ResponseWriter, r *http.Request) {
	if !f.hasSession(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if f.mintFails {
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	if f.omitMintedToken {
		_, _ = fmt.Fprint(w, `<html><body>Copy this token now — it won't be shown again, but there's no code tag.</body></html>`)
		return
	}
	_, _ = fmt.Fprint(w, `<html><body><p>Copy this token now — it won't be shown again.</p>`+
		`<code class="mt-1 block break-all font-mono">gc_u_faketokenvalue1234567890</code></body></html>`)
}

func newJarClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	return &http.Client{Jar: jar}
}

// --- fetchCLIToken: the HTTP flow itself ---

func TestFetchCLIToken_Success(t *testing.T) {
	srv := newFakeAuthServer("duckwalker", "hunter2pass").start(t)
	client := newJarClient(t)

	token, err := fetchCLIToken(client, srv.URL, "duckwalker", "hunter2pass")
	if err != nil {
		t.Fatalf("fetchCLIToken: %v", err)
	}
	if token != "gc_u_faketokenvalue1234567890" {
		t.Errorf("token = %q, want the fake server's minted token", token)
	}
}

// TestFetchCLIToken_GetsBeforeEveryPost is the regression test for the
// original bug: duck login used to POST /login without GETing the login
// page first (no double-submit CSRF cookie, no csrf_token form field), so
// every login got a 403 in production before the flow ever reached
// /profile/tokens. Because fakeAuthServer enforces that same contract, any
// regression that drops a GET call here fails this test with the exact
// error users used to see.
func TestFetchCLIToken_GetsBeforeEveryPost(t *testing.T) {
	fake := newFakeAuthServer("duckwalker", "hunter2pass")
	srv := fake.start(t)
	client := newJarClient(t)

	if _, err := fetchCLIToken(client, srv.URL, "duckwalker", "hunter2pass"); err != nil {
		t.Fatalf("fetchCLIToken: %v", err)
	}

	// The client follows the 303 redirect POST /login issues (matching real
	// server behavior), so an incidental "GET /" lands between "POST /login"
	// and "GET /profile" — assert relative order, not an exact transcript.
	got := fake.recordedRequests()
	want := []string{
		"GET /login",
		"POST /login",
		"GET /profile",
		"POST /profile/tokens",
	}
	pos := 0
	for _, req := range got {
		if pos < len(want) && req == want[pos] {
			pos++
		}
	}
	if pos != len(want) {
		t.Fatalf("requests = %v, want this order (possibly interleaved with extras): %v", got, want)
	}
}

func TestFetchCLIToken_WrongPassword(t *testing.T) {
	fake := newFakeAuthServer("duckwalker", "hunter2pass")
	srv := fake.start(t)
	client := newJarClient(t)

	_, err := fetchCLIToken(client, srv.URL, "duckwalker", "wrongpass")
	if err == nil {
		t.Fatal("fetchCLIToken: want error for wrong password, got nil")
	}
	if !strings.Contains(err.Error(), "login failed") {
		t.Errorf("err = %q, want it to mention login failure", err.Error())
	}

	// A failed login must not attempt to mint a token off a session that
	// doesn't exist.
	for _, req := range fake.recordedRequests() {
		if req == "GET /profile" || req == "POST /profile/tokens" {
			t.Errorf("unexpected request after failed login: %s", req)
		}
	}
}

func TestFetchCLIToken_LoginServerError(t *testing.T) {
	fake := newFakeAuthServer("duckwalker", "hunter2pass")
	fake.loginFails = true
	srv := fake.start(t)
	client := newJarClient(t)

	_, err := fetchCLIToken(client, srv.URL, "duckwalker", "hunter2pass")
	if err == nil || !strings.Contains(err.Error(), "server said 500") {
		t.Fatalf("err = %v, want a \"login failed: server said 500\" error", err)
	}
	// A server outage must not be misdiagnosed as bad credentials.
	if strings.Contains(err.Error(), "username") {
		t.Errorf("err = %q blames credentials for a server error", err.Error())
	}
}

func TestFetchCLIToken_LoginPageMissingCSRFField(t *testing.T) {
	fake := newFakeAuthServer("duckwalker", "hunter2pass")
	fake.omitLoginCSRF = true
	srv := fake.start(t)
	client := newJarClient(t)

	_, err := fetchCLIToken(client, srv.URL, "duckwalker", "hunter2pass")
	if err == nil || !strings.Contains(err.Error(), "login page") {
		t.Fatalf("err = %v, want a \"couldn't find CSRF token on login page\" error", err)
	}
}

func TestFetchCLIToken_ProfileFetchFails(t *testing.T) {
	fake := newFakeAuthServer("duckwalker", "hunter2pass")
	fake.brokenProfile = true
	srv := fake.start(t)
	client := newJarClient(t)

	_, err := fetchCLIToken(client, srv.URL, "duckwalker", "hunter2pass")
	if err == nil || !strings.Contains(err.Error(), "fetch profile") {
		t.Fatalf("err = %v, want a \"fetch profile\" error", err)
	}
}

func TestFetchCLIToken_ProfileMissingCSRFField(t *testing.T) {
	fake := newFakeAuthServer("duckwalker", "hunter2pass")
	fake.omitProfileCSRF = true
	srv := fake.start(t)
	client := newJarClient(t)

	_, err := fetchCLIToken(client, srv.URL, "duckwalker", "hunter2pass")
	if err == nil || !strings.Contains(err.Error(), "profile page") {
		t.Fatalf("err = %v, want a \"couldn't find CSRF token in profile page\" error", err)
	}
}

func TestFetchCLIToken_MintFails(t *testing.T) {
	fake := newFakeAuthServer("duckwalker", "hunter2pass")
	fake.mintFails = true
	srv := fake.start(t)
	client := newJarClient(t)

	_, err := fetchCLIToken(client, srv.URL, "duckwalker", "hunter2pass")
	if err == nil || !strings.Contains(err.Error(), "mint token failed") {
		t.Fatalf("err = %v, want a \"mint token failed\" error", err)
	}
}

func TestFetchCLIToken_MintResponseMissingToken(t *testing.T) {
	fake := newFakeAuthServer("duckwalker", "hunter2pass")
	fake.omitMintedToken = true
	srv := fake.start(t)
	client := newJarClient(t)

	_, err := fetchCLIToken(client, srv.URL, "duckwalker", "hunter2pass")
	if err == nil || !strings.Contains(err.Error(), "couldn't find token") {
		t.Fatalf("err = %v, want a \"couldn't find token in response\" error", err)
	}
}

func TestFetchCLIToken_TrailingSlashBase(t *testing.T) {
	srv := newFakeAuthServer("duckwalker", "hunter2pass").start(t)
	client := newJarClient(t)

	token, err := fetchCLIToken(client, srv.URL+"/", "duckwalker", "hunter2pass")
	if err != nil {
		t.Fatalf("fetchCLIToken with trailing slash: %v", err)
	}
	if token != "gc_u_faketokenvalue1234567890" {
		t.Errorf("token = %q, want the fake server's minted token", token)
	}
}

// --- loginCmd: full command, including the credential prompt and the
// token-file side effect ---

func TestLoginCmd_EndToEnd(t *testing.T) {
	srv := newFakeAuthServer("duckwalker", "hunter2pass").start(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	origStdin, origReadPassword := loginStdin, loginReadPassword
	t.Cleanup(func() { loginStdin, loginReadPassword = origStdin, origReadPassword })
	loginStdin = bytes.NewBufferString("duckwalker\n")
	loginReadPassword = func() ([]byte, error) { return []byte("hunter2pass"), nil }

	if err := loginCmd([]string{"--base", srv.URL}); err != nil {
		t.Fatalf("loginCmd: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(home, ".config", "duck", "token"))
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	if string(got) != "gc_u_faketokenvalue1234567890" {
		t.Errorf("saved token = %q, want the fake server's minted token", got)
	}

	info, err := os.Stat(filepath.Join(home, ".config", "duck", "token"))
	if err != nil {
		t.Fatalf("stat token file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("token file mode = %o, want 0600", perm)
	}
}

func TestLoginCmd_WrongPassword_NoTokenFileWritten(t *testing.T) {
	srv := newFakeAuthServer("duckwalker", "hunter2pass").start(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	origStdin, origReadPassword := loginStdin, loginReadPassword
	t.Cleanup(func() { loginStdin, loginReadPassword = origStdin, origReadPassword })
	loginStdin = bytes.NewBufferString("duckwalker\n")
	loginReadPassword = func() ([]byte, error) { return []byte("wrongpass"), nil }

	err := loginCmd([]string{"--base", srv.URL})
	if err == nil {
		t.Fatal("loginCmd: want error for wrong password, got nil")
	}

	if _, statErr := os.Stat(filepath.Join(home, ".config", "duck", "token")); !os.IsNotExist(statErr) {
		t.Errorf("token file should not exist after a failed login, stat err = %v", statErr)
	}
}

func TestLoginCmd_UsageErrorOnExtraArgs(t *testing.T) {
	if err := loginCmd([]string{"unexpected-positional-arg"}); err == nil {
		t.Fatal("loginCmd: want usage error for an unexpected positional arg, got nil")
	}
}

func TestLoginCmd_ReadUsernameError(t *testing.T) {
	origStdin := loginStdin
	t.Cleanup(func() { loginStdin = origStdin })
	loginStdin = bytes.NewBufferString("") // EOF immediately

	if err := loginCmd(nil); err == nil || !strings.Contains(err.Error(), "read username") {
		t.Fatalf("loginCmd with empty stdin: err = %v, want a \"read username\" error", err)
	}
}

func TestLoginCmd_ReadPasswordError(t *testing.T) {
	origStdin, origReadPassword := loginStdin, loginReadPassword
	t.Cleanup(func() { loginStdin, loginReadPassword = origStdin, origReadPassword })
	loginStdin = bytes.NewBufferString("duckwalker\n")
	loginReadPassword = func() ([]byte, error) { return nil, fmt.Errorf("inappropriate ioctl for device") }

	if err := loginCmd(nil); err == nil || !strings.Contains(err.Error(), "read password") {
		t.Fatalf("loginCmd with failing password read: err = %v, want a \"read password\" error", err)
	}
}

func TestLoginCmd_UnknownFlagErrorIsSurfaced(t *testing.T) {
	err := loginCmd([]string{"--bse", "http://localhost:1"})
	if err == nil || !strings.Contains(err.Error(), "-bse") {
		t.Fatalf("err = %v, want the flag error to name the unknown flag -bse", err)
	}
}
