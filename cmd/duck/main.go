// Command duck is the local companion CLI. Learners pull a course's
// challenges, run tests with their own toolchain (no Docker), and submit
// solutions to the server for a graded score; authors round-trip a course
// variant's markdown with `duck educator pull/lint/push`.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		// A command that already printed its help (missing a required arg,
		// unknown command) exits non-zero without a second, redundant line.
		if errors.Is(err, errHelpShown) {
			os.Exit(2)
		}
		fmt.Fprintln(os.Stderr, "duck:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	// Bare `duck` prints the full help (exit 0) rather than a terse usage line.
	if len(args) == 0 {
		return helpCmd(nil)
	}
	// Explicit top-level help: `duck help [topic...]`, `duck --help`, `duck -h`.
	if isHelpArg(args[0]) {
		return helpCmd(args[1:])
	}
	// `duck <cmd> --help` / `duck <cmd> -h` → that command's detailed help.
	// The command groups (educator, auth) are excluded so their own dispatch
	// can handle `--help` for both the group and its subcommands.
	if args[0] != "educator" && args[0] != "ed" && args[0] != "auth" && hasHelpFlag(args[1:]) {
		return helpCmd(args[:1])
	}
	switch args[0] {
	case "pull":
		return pullCmd(args[1:])
	case "test":
		return testCmd(args[1:])
	case "submit":
		return submitCmd(args[1:])
	case "propose":
		return proposeCmd(args[1:])
	case "proposals":
		return proposalsCmd(args[1:])
	case "auth":
		return authCmd(args[1:])
	case "login":
		// Deprecated spelling, kept so existing muscle memory and scripts
		// don't break the day auth grew subcommands.
		fmt.Fprintln(os.Stderr, "duck: `duck login` is now `duck auth login` — this alias will be removed in a future release")
		return authLoginCmd(args[1:])
	case "version":
		return versionCmd(args[1:])
	case "educator", "ed":
		return educatorCmd(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "duck: unknown command %q\n\n", args[0])
		return usageHelp()
	}
}

// parseInterleaved parses fs accepting flags before AND after positional
// arguments (`duck pull intro-to-go/go --base URL`), which stdlib flag
// alone doesn't: it stops at the first positional. Returns the positionals
// in order.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		i := 0
		for i < len(rest) && (!strings.HasPrefix(rest[i], "-") || rest[i] == "-") {
			pos = append(pos, rest[i])
			i++
		}
		if i == len(rest) {
			return pos, nil
		}
		args = rest[i:]
	}
}
