package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMaskToken(t *testing.T) {
	cases := []struct {
		token string
		want  string
	}{
		{"gc_u_bd856f2101d7f2fe6034ce9a5c2438be534156eb", "gc_u_bd85…56eb"},
		{"gc_u_short", "gc_u…"}, // too short to show head+tail without leaking most of it
		{"x", "x…"},
	}
	for _, c := range cases {
		if got := maskToken(c.token); got != c.want {
			t.Errorf("maskToken(%q) = %q, want %q", c.token, got, c.want)
		}
	}
	// Masking must never echo a full-length token back.
	full := "gc_u_bd856f2101d7f2fe6034ce9a5c2438be534156eb"
	if strings.Contains(maskToken(full), full[5:25]) {
		t.Errorf("maskToken leaks the token body: %q", maskToken(full))
	}
}

// fakeStatusServer answers GET /profile like the real server's requireUser:
// 200 for the accepted bearer token, 401 for anything else.
func fakeStatusServer(t *testing.T, accept string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /profile", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer "+accept {
			_, _ = fmt.Fprint(w, "<html><body>profile</body></html>")
			return
		}
		http.Error(w, "invalid or revoked token", http.StatusUnauthorized)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// writeTokenFile puts token at $HOME/.config/duck/token under a temp HOME
// and returns that HOME.
func writeTokenFile(t *testing.T, token string) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "duck")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "token"), []byte(token+"\n"), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	return home
}

// captureStdout runs f with os.Stdout redirected and returns what it printed.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
	f()
	_ = w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	return string(out)
}

func TestAuthStatus_TokenFileAccepted(t *testing.T) {
	srv := fakeStatusServer(t, "gc_u_goodtoken")
	t.Setenv("HOME", writeTokenFile(t, "gc_u_goodtoken"))
	t.Setenv("DUCK_TOKEN", "")

	var err error
	out := captureStdout(t, func() { err = authStatusCmd([]string{"--base", srv.URL}) })
	if err != nil {
		t.Fatalf("authStatusCmd: %v", err)
	}
	for _, want := range []string{"logged in", tokenSourceFile} {
		if !strings.Contains(out, want) {
			t.Errorf("status output missing %q:\n%s", want, out)
		}
	}
}

// TestAuthStatus_StaleEnvTokenShadowsFreshFile is the regression test for
// the original mystery: a fresh `duck auth login` saved a working token, but
// a stale DUCK_TOKEN kept being sent instead. status must (a) send the env
// token, (b) call out that the differing file token is NOT being used, and
// (c) tell the user unsetting DUCK_TOKEN is a fix.
func TestAuthStatus_StaleEnvTokenShadowsFreshFile(t *testing.T) {
	srv := fakeStatusServer(t, "gc_u_goodtoken")
	t.Setenv("HOME", writeTokenFile(t, "gc_u_goodtoken"))
	t.Setenv("DUCK_TOKEN", "gc_u_stalerevokedtoken")

	var err error
	out := captureStdout(t, func() { err = authStatusCmd([]string{"--base", srv.URL}) })
	if err == nil {
		t.Fatal("authStatusCmd: want rejection error, got nil")
	}
	if !strings.Contains(err.Error(), "unset DUCK_TOKEN") {
		t.Errorf("err = %q, want it to suggest unsetting DUCK_TOKEN", err.Error())
	}
	for _, want := range []string{tokenSourceEnv, "NOT being used"} {
		if !strings.Contains(out, want) {
			t.Errorf("status output missing %q:\n%s", want, out)
		}
	}
}

func TestAuthStatus_FileTokenRejected(t *testing.T) {
	srv := fakeStatusServer(t, "gc_u_goodtoken")
	t.Setenv("HOME", writeTokenFile(t, "gc_u_revokedtoken"))
	t.Setenv("DUCK_TOKEN", "")

	var err error
	_ = captureStdout(t, func() { err = authStatusCmd([]string{"--base", srv.URL}) })
	if err == nil || !strings.Contains(err.Error(), "duck auth login") {
		t.Fatalf("err = %v, want a rejection pointing at `duck auth login`", err)
	}
	// The file token wasn't shadowed by anything, so don't suggest unsetting
	// a DUCK_TOKEN that isn't set.
	if strings.Contains(err.Error(), "unset DUCK_TOKEN") {
		t.Errorf("err = %q suggests unsetting DUCK_TOKEN, but no DUCK_TOKEN was involved", err.Error())
	}
}

func TestAuthStatus_NoTokenAnywhere(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DUCK_TOKEN", "")

	var err error
	_ = captureStdout(t, func() { err = authStatusCmd(nil) })
	if err == nil || !strings.Contains(err.Error(), "duck auth login") {
		t.Fatalf("err = %v, want a no-token error pointing at `duck auth login`", err)
	}
}

// TestAuthStatus_RedirectIsNotMisreadAsRevoked: a server that redirects
// (e.g. www → apex) would strip the Authorization header if followed, making
// a healthy token look revoked. status must instead surface the redirect and
// the address to use.
func TestAuthStatus_RedirectIsNotMisreadAsRevoked(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /profile", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://duckgc.example/profile", http.StatusPermanentRedirect)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Setenv("HOME", writeTokenFile(t, "gc_u_goodtoken"))
	t.Setenv("DUCK_TOKEN", "")

	var err error
	_ = captureStdout(t, func() { err = authStatusCmd([]string{"--base", srv.URL}) })
	if err == nil || !strings.Contains(err.Error(), "redirects to") {
		t.Fatalf("err = %v, want a redirect diagnosis", err)
	}
	if err != nil && strings.Contains(err.Error(), "revoked") {
		t.Errorf("err = %q misreads a redirect as a revoked token", err.Error())
	}
}

func TestAuthStatus_UsageErrorOnExtraArgs(t *testing.T) {
	if err := authStatusCmd([]string{"unexpected"}); err == nil {
		t.Fatal("authStatusCmd: want usage error for an unexpected positional arg, got nil")
	}
}

// TestAuthLoginCmd_HonorsDuckBaseURL: pull and educator have always read
// DUCK_BASE_URL; login minting its token on a *different* server than the
// one every other command talks to was a straight path to an unauthorized
// submit after a perfectly good login.
func TestAuthLoginCmd_HonorsDuckBaseURL(t *testing.T) {
	srv := newFakeAuthServer("duckwalker", "hunter2pass").start(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DUCK_BASE_URL", srv.URL)
	t.Setenv("DUCK_TOKEN", "")

	origStdin, origReadPassword := loginStdin, loginReadPassword
	t.Cleanup(func() { loginStdin, loginReadPassword = origStdin, origReadPassword })
	loginStdin = bytes.NewBufferString("duckwalker\n")
	loginReadPassword = func() ([]byte, error) { return []byte("hunter2pass"), nil }

	var err error
	_ = captureStdout(t, func() { err = authLoginCmd(nil) })
	if err != nil {
		t.Fatalf("authLoginCmd: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(home, ".config", "duck", "token"))
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	if string(got) != "gc_u_faketokenvalue1234567890" {
		t.Errorf("saved token = %q, want the fake server's minted token", got)
	}
}

// TestAuthLoginCmd_WarnsWhenDuckTokenShadows: saving a token that will never
// be used must not be silent.
func TestAuthLoginCmd_WarnsWhenDuckTokenShadows(t *testing.T) {
	srv := newFakeAuthServer("duckwalker", "hunter2pass").start(t)

	t.Setenv("HOME", t.TempDir())
	t.Setenv("DUCK_BASE_URL", "")
	t.Setenv("DUCK_TOKEN", "gc_u_someoldtoken")

	origStdin, origReadPassword := loginStdin, loginReadPassword
	t.Cleanup(func() { loginStdin, loginReadPassword = origStdin, origReadPassword })
	loginStdin = bytes.NewBufferString("duckwalker\n")
	loginReadPassword = func() ([]byte, error) { return []byte("hunter2pass"), nil }

	var err error
	out := captureStdout(t, func() { err = authLoginCmd([]string{"--base", srv.URL}) })
	if err != nil {
		t.Fatalf("authLoginCmd: %v", err)
	}
	if !strings.Contains(out, "DUCK_TOKEN") {
		t.Errorf("login output should warn about the shadowing DUCK_TOKEN:\n%s", out)
	}
}

// TestAuthCmd_Dispatch covers the auth group's routing edges; the login and
// status behaviors themselves are tested above.
func TestAuthCmd_Dispatch(t *testing.T) {
	if err := authCmd([]string{"bogus"}); err == nil {
		t.Error("authCmd(bogus): want usage error, got nil")
	}
	if err := authCmd(nil); err == nil {
		t.Error("authCmd(): want usage error for missing subcommand, got nil")
	}
}
