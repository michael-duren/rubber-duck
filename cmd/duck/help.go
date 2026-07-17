package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
)

// This file is the single source of truth for `duck`'s help text. The
// registry below drives both the top-level command list (`duck help`) and
// the per-command detail (`duck <cmd> --help`), so the two can't drift.
// Dispatch still lives in run()/educatorCmd — help is intercepted before it.

// flagHelp documents one flag or environment variable: the token as typed
// and a one-line description.
type flagHelp struct {
	name string
	desc string
}

// example is one runnable invocation plus a line explaining what it does.
type example struct {
	cmd  string
	desc string
}

// cmdHelp is the help entry for one command (or educator subcommand).
type cmdHelp struct {
	name     string     // lookup key, e.g. "pull" or "educator"
	title    string     // header line, e.g. "duck educator pull"
	summary  string     // one-liner shown in the top-level command list
	usage    string     // usage synopsis
	long     string     // detailed description (may be multiple lines)
	flags    []flagHelp // command-specific flags
	envs     []flagHelp // command-specific environment variables
	examples []example
	subs     []cmdHelp // subcommands (educator only)
}

// baseFlag is repeated across the network commands, so name it once.
var baseFlag = flagHelp{
	"--base URL",
	"server base URL (default: $DUCK_BASE_URL, else https://duckgc.com)",
}

// commands is the ordered registry of top-level commands.
var commands = []cmdHelp{
	{
		name:    "pull",
		title:   "duck pull",
		summary: "Download a course's challenges into local starter files",
		usage:   "duck pull <course>/<language> [--base URL]",
		long: "Scaffolds a course variant into ./<course>-<language>/, one\n" +
			"subdirectory per challenge holding the starter code and its test\n" +
			"file (plus a go.mod for Go). Directories are prefixed with their\n" +
			"lesson number (\"03-merge\"; \"05a-min-heap\"/\"05b-heapsort\" when a\n" +
			"lesson has several; \"final-…\" for the final challenge) so a\n" +
			"listing sorts in course order. A .duck-course.json is written at\n" +
			"the root so `duck test` and `duck submit` work from anywhere in the\n" +
			"tree. Challenges that already exist locally are skipped, so pulling\n" +
			"again never clobbers work in progress.",
		flags: []flagHelp{baseFlag},
		examples: []example{
			{"duck pull intro-to-go/go", "pull the Go variant into ./intro-to-go-go/"},
			{"duck pull intro-to-go/python", "pull the Python variant"},
			{"duck pull intro-to-go/go --base http://localhost:8080", "pull from a local dev server"},
		},
	},
	{
		name:    "test",
		title:   "duck test",
		summary: "Run challenge tests locally with your own toolchain",
		usage:   "duck test [challenge-slug]",
		long: "Runs a challenge's tests on your machine using the native\n" +
			"toolchain (go, python3, or cc) — no Docker, no network. Run it from\n" +
			"anywhere inside a pulled course tree. With no slug it runs every\n" +
			"challenge; with a slug (or its directory name — \"merge\" and\n" +
			"\"03-merge\" both work) it runs just that one. Each run is bounded by\n" +
			"a 30s timeout and reports PASS, FAIL, or TIMEOUT with the test\n" +
			"output.",
		examples: []example{
			{"duck test", "run every challenge in the course"},
			{"duck test hello-world", "run only the hello-world challenge"},
		},
	},
	{
		name:    "submit",
		title:   "duck submit",
		summary: "Grade a solution locally and record your score",
		usage:   "duck submit <challenge-slug> [--remote]",
		long: "Local-first grading: runs the challenge's tests on your machine,\n" +
			"submits the code together with the claimed pass/fail verdict, and\n" +
			"prints the result immediately. The challenge can be named by slug\n" +
			"or by its pulled directory (\"merge\" and \"03-merge\" both work). The\n" +
			"server re-grades in the background as an audit nobody waits on.\n" +
			"Requires a saved token (see `duck auth login`). If the language's\n" +
			"toolchain isn't installed, submit automatically falls back to\n" +
			"synchronous server grading.",
		flags: []flagHelp{
			{"--remote", "skip the local run; grade on the server and wait for the result"},
		},
		examples: []example{
			{"duck submit hello-world", "grade locally, then record the verdict"},
			{"duck submit hello-world --remote", "force server-side grading (slower, authoritative)"},
		},
	},
	{
		name:    "auth",
		title:   "duck auth",
		summary: "Manage authentication: log in and check token status",
		usage:   "duck auth <login|status> [args]",
		long: "Authentication for the commands that write to the server (`duck\n" +
			"submit`, all of `duck educator`). `login` mints a token and saves it\n" +
			"to ~/.config/duck/token; `status` shows which token duck would send\n" +
			"— DUCK_TOKEN beats the token file when both exist — and whether the\n" +
			"server accepts it. When a submit says \"unauthorized\", start with\n" +
			"`duck auth status`.",
		examples: []example{
			{"duck auth login", "log in to https://duckgc.com and save a token"},
			{"duck auth status", "show which token duck uses and whether it works"},
		},
		subs: []cmdHelp{
			{
				name:    "login",
				title:   "duck auth login",
				summary: "Authenticate and save an API token",
				usage:   "duck auth login [--base URL]",
				long: "Prompts for your username and password, mints an API token, and\n" +
					"saves it to ~/.config/duck/token (mode 0600). `duck submit` and the\n" +
					"`duck educator` commands read that token. As an alternative to\n" +
					"logging in, set the DUCK_TOKEN environment variable to a token\n" +
					"minted on the website's profile page — but note DUCK_TOKEN takes\n" +
					"precedence over the saved file whenever it is set.",
				flags: []flagHelp{baseFlag},
				examples: []example{
					{"duck auth login", "log in to https://duckgc.com"},
					{"duck auth login --base http://localhost:8080", "log in to a local dev server"},
				},
			},
			{
				name:    "status",
				title:   "duck auth status",
				summary: "Show which token duck would use and whether the server accepts it",
				usage:   "duck auth status [--base URL]",
				long: "Reports the server, the token duck would send and where it came\n" +
					"from (DUCK_TOKEN wins over ~/.config/duck/token when both are\n" +
					"set), then asks the server whether it accepts that token. Exits\n" +
					"non-zero when it doesn't. This is the first thing to run when a\n" +
					"submit or push answers \"unauthorized\" right after a login: the\n" +
					"usual culprits — a stale DUCK_TOKEN shadowing the fresh token\n" +
					"file, or a token minted on a different server — are all visible\n" +
					"here.",
				flags: []flagHelp{baseFlag},
				examples: []example{
					{"duck auth status", "check the token against https://duckgc.com"},
					{"duck auth status --base http://localhost:8080", "check against a local dev server"},
				},
			},
		},
	},
	{
		name:    "educator",
		title:   "duck educator",
		summary: "Author a course: pull, lint, and push its markdown (alias: ed)",
		usage:   "duck educator <pull|push|lint> [args]",
		long: "Author-side workflow for course content. A course variant is a\n" +
			"single markdown document; these commands round-trip it with the\n" +
			"server. `pull` fetches the markdown plus a .meta.json sidecar that\n" +
			"records its version; you edit locally; `lint` validates it without\n" +
			"the server; `push` sends it back, using the recorded version for\n" +
			"optimistic concurrency so you can't silently overwrite someone\n" +
			"else's changes. `pull` and `push` need a saved token (see\n" +
			"`duck auth login`); `lint` runs entirely offline.\n" +
			"`ed` is an accepted alias for `educator`.",
		examples: []example{
			{"duck educator pull intro-to-go/go", "fetch the Go variant's markdown to edit"},
			{"duck ed lint", "validate the pulled file in the current directory"},
			{"duck ed push", "push your edits back to the server"},
		},
		subs: []cmdHelp{
			{
				name:    "pull",
				title:   "duck educator pull",
				summary: "Fetch a course variant's markdown plus its version sidecar",
				usage:   "duck educator pull <course>/<language> [--base URL] [--force]",
				long: "Fetches the variant's markdown to ./<course>-<language>.md and\n" +
					"writes a <file>.meta.json sidecar recording the server URL,\n" +
					"course, language, and version. To protect unpushed work, pull\n" +
					"refuses to overwrite a local file whose contents differ from the\n" +
					"server's unless you pass --force.",
				flags: []flagHelp{
					baseFlag,
					{"--force", "overwrite a local file that differs from the server's copy"},
				},
				examples: []example{
					{"duck educator pull intro-to-go/go", "fetch intro-to-go-go.md + sidecar"},
					{"duck educator pull intro-to-go/go --force", "re-fetch, discarding local edits"},
				},
			},
			{
				name:    "lint",
				title:   "duck educator lint",
				summary: "Validate a course markdown file locally (no server round trip)",
				usage:   "duck educator lint [file]",
				long: "Runs the same validation the server would, entirely offline, and\n" +
					"prints any problems with their line numbers. With no file\n" +
					"argument it lints the single sidecar-tracked file in the current\n" +
					"directory.",
				examples: []example{
					{"duck educator lint", "lint the pulled file in this directory"},
					{"duck educator lint intro-to-go-go.md", "lint a specific file"},
				},
			},
			{
				name:    "push",
				title:   "duck educator push",
				summary: "Lint, then upload a course markdown file back to the server",
				usage:   "duck educator push [file]",
				long: "Lints the file locally, then PUTs it back using the sidecar's\n" +
					"recorded version for optimistic concurrency. If someone changed\n" +
					"the variant since you pulled, the push is rejected and duck tells\n" +
					"you how to reconcile. On success the sidecar's version is bumped\n" +
					"to match. With no file argument it pushes the single\n" +
					"sidecar-tracked file in the current directory.",
				examples: []example{
					{"duck educator push", "push the pulled file in this directory"},
					{"duck educator push intro-to-go-go.md", "push a specific file"},
				},
			},
		},
	},
	{
		name:    "version",
		title:   "duck version",
		summary: "Print the duck version",
		usage:   "duck version",
		long:    "Prints the version of the duck CLI.",
		examples: []example{
			{"duck version", "print the version string"},
		},
	},
	{
		name:    "help",
		title:   "duck help",
		summary: "Show help for duck or a specific command",
		usage:   "duck help [command]",
		long: "With no argument, lists every command. With a command name, prints\n" +
			"that command's detailed help — the same as `duck <command> --help`.",
		examples: []example{
			{"duck help", "list all commands"},
			{"duck help submit", "show detailed help for submit"},
			{"duck help educator pull", "show detailed help for an educator subcommand"},
		},
	},
}

// globalEnv documents environment variables that affect duck as a whole,
// shown at the bottom of the top-level help.
var globalEnv = []flagHelp{
	{"DUCK_BASE_URL", "default server base URL for pull/auth/educator commands"},
	{"DUCK_TOKEN", "API token, overriding ~/.config/duck/token"},
}

// findCmd looks up a top-level command by name or alias.
func findCmd(name string) *cmdHelp {
	for i := range commands {
		if commands[i].name == name {
			return &commands[i]
		}
	}
	if name == "ed" {
		return findCmd("educator")
	}
	// `duck login` is a deprecated alias for `duck auth login`; keep its
	// help topic resolving so `duck login --help` still teaches the new
	// spelling instead of erroring.
	if name == "login" {
		return findSub(findCmd("auth"), "login")
	}
	return nil
}

// findSub looks up a subcommand of c by name.
func findSub(c *cmdHelp, name string) *cmdHelp {
	for i := range c.subs {
		if c.subs[i].name == name {
			return &c.subs[i]
		}
	}
	return nil
}

// isHelpArg reports whether a single token requests help.
func isHelpArg(a string) bool {
	return a == "-h" || a == "--help" || a == "help"
}

// hasHelpFlag reports whether any of args is a help flag (-h/--help). Unlike
// isHelpArg it does not treat a bare "help" positional as a request, so a
// challenge slug literally named "help" still reaches its command.
func hasHelpFlag(args []string) bool {
	return slices.Contains(args, "-h") || slices.Contains(args, "--help")
}

// errHelpShown signals that a command already printed its detailed help —
// e.g. it was invoked with a required argument missing. main() maps it to a
// usage exit code without printing anything further.
var errHelpShown = errors.New("help shown")

// helpCmd renders help for a topic to stdout in response to an explicit
// request (`duck help ...`, `duck <cmd> --help`) and exits 0.
func helpCmd(topic []string) error {
	return renderTopic(os.Stdout, topic)
}

// usageHelp prints the detailed help for topic to stderr and returns
// errHelpShown, so a command missing a required argument shows the user how
// to invoke it (and exits non-zero) instead of printing a terse one-liner.
func usageHelp(topic ...string) error {
	_ = renderTopic(os.Stderr, topic)
	return errHelpShown
}

// renderTopic writes help for a topic path to w: [] for the top-level list,
// [cmd] for one command, or [cmd, sub] for a subcommand. It returns an error
// only when the topic itself is unknown.
func renderTopic(w io.Writer, topic []string) error {
	switch len(topic) {
	case 0:
		renderMainHelp(w)
		return nil
	case 1:
		c := findCmd(topic[0])
		if c == nil {
			return fmt.Errorf("unknown help topic %q — run `duck help`", topic[0])
		}
		renderCmdHelp(w, *c)
		return nil
	default:
		c := findCmd(topic[0])
		if c == nil {
			return fmt.Errorf("unknown help topic %q — run `duck help`", topic[0])
		}
		sc := findSub(c, topic[1])
		if sc == nil {
			return fmt.Errorf("unknown help topic %q %q — run `duck help %s`", topic[0], topic[1], topic[0])
		}
		renderCmdHelp(w, *sc)
		return nil
	}
}

// renderMainHelp writes the top-level command list.
func renderMainHelp(w io.Writer) {
	_, _ = fmt.Fprintln(w, "duck — local companion CLI for Rubber Duck courses")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Learners pull a course's challenges, run tests with their own")
	_, _ = fmt.Fprintln(w, "toolchain, and submit graded solutions. Authors round-trip a course's")
	_, _ = fmt.Fprintln(w, "markdown with `duck educator`.")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintln(w, "  duck <command> [args]")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Commands:")
	width := 0
	for _, c := range commands {
		width = max(width, len(c.name))
	}
	for _, c := range commands {
		_, _ = fmt.Fprintf(w, "  %-*s  %s\n", width, c.name, c.summary)
	}
	_, _ = fmt.Fprintln(w)
	renderFlagBlock(w, "Environment:", globalEnv)
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Run \"duck help <command>\" or \"duck <command> --help\" for details.")
}

// renderCmdHelp writes the detailed help for one command or subcommand.
func renderCmdHelp(w io.Writer, c cmdHelp) {
	_, _ = fmt.Fprintf(w, "%s — %s\n\n", c.title, c.summary)
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintf(w, "  %s\n", c.usage)
	if c.long != "" {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, c.long)
	}
	if len(c.subs) > 0 {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, "Subcommands:")
		width := 0
		for _, s := range c.subs {
			width = max(width, len(s.name))
		}
		for _, s := range c.subs {
			_, _ = fmt.Fprintf(w, "  %-*s  %s\n", width, s.name, s.summary)
		}
	}
	if len(c.flags) > 0 {
		_, _ = fmt.Fprintln(w)
		renderFlagBlock(w, "Flags:", c.flags)
	}
	if len(c.envs) > 0 {
		_, _ = fmt.Fprintln(w)
		renderFlagBlock(w, "Environment:", c.envs)
	}
	if len(c.examples) > 0 {
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, "Examples:")
		for _, e := range c.examples {
			_, _ = fmt.Fprintf(w, "  %s\n", e.cmd)
			_, _ = fmt.Fprintf(w, "      %s\n", e.desc)
		}
	}
}

// renderFlagBlock writes a titled, column-aligned list of name/description
// pairs (used for both flags and environment variables).
func renderFlagBlock(w io.Writer, title string, items []flagHelp) {
	_, _ = fmt.Fprintln(w, title)
	width := 0
	for _, it := range items {
		width = max(width, len(it.name))
	}
	for _, it := range items {
		_, _ = fmt.Fprintf(w, "  %-*s  %s\n", width, it.name, it.desc)
	}
}
