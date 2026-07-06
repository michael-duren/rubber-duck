// Command duck is the local-testing companion CLI: pull a course's
// challenges, run tests with your own toolchain (no Docker), and submit
// solutions to the server for a graded score.
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
		fmt.Fprintln(os.Stderr, "duck:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errUsageError
	}
	switch args[0] {
	case "pull":
		return pullCmd(args[1:])
	case "test":
		return testCmd(args[1:])
	case "submit":
		return submitCmd(args[1:])
	case "login":
		return loginCmd(args[1:])
	case "version":
		return versionCmd(args[1:])
	default:
		return errUsageError
	}
}

var errUsageError = errors.New("usage: duck <pull|test|submit|login|version> [args]")

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
