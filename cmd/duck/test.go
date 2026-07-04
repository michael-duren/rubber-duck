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

		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)

		var cmd *exec.Cmd
		switch meta.Language {
		case "go":
			cmd = exec.CommandContext(ctx, "go", "test", "-timeout=25s", "./...")
		case "python":
			cmd = exec.CommandContext(ctx, "python3", "-m", "pytest", "-q")
		case "c":
			cmd = exec.CommandContext(ctx, "sh", "-c",
				"cc -std=c17 -Wall -o test_bin solution.c test_solution.c && ./test_bin")
		default:
			cancel()
			return fmt.Errorf("unsupported language %q", meta.Language)
		}
		cmd.Dir = dir
		out, runErr := cmd.CombinedOutput()
		cancel()

		status := "PASS"
		if ctx.Err() == context.DeadlineExceeded {
			status = "TIMEOUT"
			failed = true
		} else if runErr != nil {
			status = "FAIL"
			failed = true
		}
		fmt.Printf("=== %s: %s ===\n%s\n", slug, status, out)
	}
	if failed {
		return fmt.Errorf("one or more challenges failed")
	}
	return nil
}
