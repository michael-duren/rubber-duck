package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// authCmd dispatches `duck auth` subcommands. Authentication grew from a
// single top-level `duck login` into a family (login, status), so it follows
// the same group shape as `duck educator`.
func authCmd(args []string) error {
	// Bare `duck auth` needs a subcommand: show the auth help.
	if len(args) == 0 {
		return usageHelp("auth")
	}
	// `duck auth help [sub]`, `duck auth --help`, `duck auth -h`.
	if isHelpArg(args[0]) {
		return helpCmd(append([]string{"auth"}, args[1:]...))
	}
	// `duck auth <sub> --help` → that subcommand's detailed help.
	if hasHelpFlag(args[1:]) {
		return helpCmd([]string{"auth", args[0]})
	}
	switch args[0] {
	case "login":
		return authLoginCmd(args[1:])
	case "status":
		return authStatusCmd(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "duck: unknown auth subcommand %q\n\n", args[0])
		return usageHelp("auth")
	}
}

// authStatusCmd reports which token duck would send, where it came from, and
// whether the server accepts it. Its whole reason to exist is the class of
// "I just logged in, why am I unauthorized?" mysteries: a stale DUCK_TOKEN
// shadowing the token file, a token minted on a different server than the
// one being talked to, or a base URL the server redirects away from.
func authStatusCmd(args []string) error {
	fs := flag.NewFlagSet("auth status", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // suppress default help output
	baseURL := fs.String("base", envOr("DUCK_BASE_URL", "https://duckgc.com"), "server base URL")
	rest, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return usageHelp("auth", "status")
	}
	base := strings.TrimRight(*baseURL, "/")
	fmt.Printf("server: %s\n", base)

	token, source, err := loadToken()
	if err != nil {
		fmt.Println("token:  none found")
		return err
	}
	fmt.Printf("token:  %s (from %s)\n", maskToken(token), source)

	// The precedence trap, spelled out: DUCK_TOKEN wins even when a fresh
	// `duck auth login` just wrote a different token to the file.
	if source == tokenSourceEnv {
		if path, err := tokenFilePath(); err == nil {
			if b, err := os.ReadFile(path); err == nil {
				if saved := strings.TrimSpace(string(b)); saved != "" && saved != token {
					fmt.Printf("note:   %s (%s) also exists and differs — DUCK_TOKEN takes precedence, so the saved token is NOT being used\n", tokenSourceFile, maskToken(saved))
				}
			}
		}
	}

	ok, err := checkToken(base, token)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("status: rejected — the server does not accept this token (revoked, or minted on a different server); run `duck auth login`%s", unsetHint(source))
	}
	fmt.Println("status: logged in — token accepted")
	return nil
}

// unsetHint appends the DUCK_TOKEN escape hatch to a rejection message only
// when the rejected token actually came from the environment.
func unsetHint(source string) string {
	if source == tokenSourceEnv {
		return ", or unset DUCK_TOKEN to use the saved token file"
	}
	return ""
}

// checkToken asks the server whether it accepts token by GETting /profile —
// a page that answers 401 to a bad bearer token and 200 to a good one — with
// redirects disabled: following one would strip the Authorization header
// (Go's client drops it cross-host) and turn a config problem into a
// convincing but wrong "token revoked" answer.
func checkToken(base, token string) (bool, error) {
	req, err := http.NewRequest("GET", base+"/profile", nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("check token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusOK:
		return true, nil
	case resp.StatusCode == http.StatusUnauthorized:
		return false, nil
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return false, fmt.Errorf("status: unknown — the server redirects to %s; point --base (or DUCK_BASE_URL) at that address instead", resp.Header.Get("Location"))
	default:
		return false, fmt.Errorf("check token: server said %s", resp.Status)
	}
}

// maskToken keeps enough of a token visible to tell two tokens apart (the
// prefix and a few trailing characters) without printing a usable secret.
func maskToken(token string) string {
	const tail = 4
	if len(token) <= len("gc_u_")+2*tail {
		return token[:min(len(token), tail)] + "…"
	}
	return token[:len("gc_u_")+tail] + "…" + token[len(token)-tail:]
}
