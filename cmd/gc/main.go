// Command gc is the local-testing companion CLI: pull a course's
// challenges, run tests with your own toolchain (no Docker), and submit
// solutions to the server for a graded score.
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gc:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usageError
	}
	switch args[0] {
	case "pull":
		return pullCmd(args[1:])
	case "test":
		return testCmd(args[1:])
	case "submit":
		return submitCmd(args[1:])
	default:
		return usageError
	}
}

var usageError = fmt.Errorf("usage: gc <pull|test|submit> ...")
