// Command duck is the local-testing companion CLI: pull a course's
// challenges, run tests with your own toolchain (no Docker), and submit
// solutions to the server for a graded score.
package main

import (
	"errors"
	"fmt"
	"os"
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
