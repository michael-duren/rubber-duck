package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// testTimeout bounds a single challenge's test run so a deadlocking
// solution (e.g. ranging over a nil channel) can't hang the CLI forever.
const testTimeout = 30 * time.Second

// toolchainFor names the binary a language's local test run needs, so
// callers can check availability before running.
func toolchainFor(language string) (string, error) {
	switch language {
	case "go":
		return "go", nil
	case "python":
		return "python3", nil
	case "c":
		return "cc", nil
	default:
		return "", fmt.Errorf("unsupported language %q", language)
	}
}

// runResult is the outcome of one local test run.
type runResult struct {
	passed   bool
	timedOut bool
	output   string
}

// runChallenge runs one challenge's tests locally with the native
// toolchain. A non-nil error means the run couldn't happen (unsupported
// language); test failures are data, not errors.
func runChallenge(dir, language string) (runResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	// Commands mirror the server runner images (internal/grader/runners/*)
	// so a claimed verdict's output parses into the same test counts the
	// server would produce — `go test -v` in particular, since proportional
	// scoring counts --- PASS/FAIL lines.
	var cmd *exec.Cmd
	switch language {
	case "go":
		cmd = exec.CommandContext(ctx, "go", "test", "-v", "-timeout=25s", "./...")
	case "python":
		cmd = exec.CommandContext(ctx, "python3", "-m", "pytest", "-q")
	case "c":
		cmd = exec.CommandContext(ctx, "sh", "-c",
			"cc -std=c17 -Wall -o test_bin solution.c test_solution.c && ./test_bin")
	default:
		return runResult{}, fmt.Errorf("unsupported language %q", language)
	}
	cmd.Dir = dir
	out, runErr := cmd.CombinedOutput()

	res := runResult{output: string(out)}
	if ctx.Err() == context.DeadlineExceeded {
		res.timedOut = true
	} else if runErr == nil {
		res.passed = true
	}
	return res, nil
}

func testCmd(args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: duck test [challenge-slug]")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, meta, err := findCourseRoot(cwd)
	if err != nil {
		return err
	}

	var slugs []string
	if len(args) == 1 {
		slugs = []string{args[0]}
	} else {
		entries, err := os.ReadDir(root)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.IsDir() {
				slugs = append(slugs, e.Name())
			}
		}
	}
	if len(slugs) == 0 {
		return fmt.Errorf("no challenges found under %s", root)
	}

	failed := false
	for _, slug := range slugs {
		dir := filepath.Join(root, slug)
		if _, err := os.Stat(dir); err != nil {
			return fmt.Errorf("no such challenge dir %s", dir)
		}

		res, err := runChallenge(dir, meta.Language)
		if err != nil {
			return err
		}
		status := "PASS"
		switch {
		case res.timedOut:
			status = "TIMEOUT"
			failed = true
		case !res.passed:
			status = "FAIL"
			failed = true
		}
		fmt.Printf("=== %s: %s ===\n%s\n", slug, status, res.output)
	}
	if failed {
		return fmt.Errorf("one or more challenges failed")
	}
	return nil
}
