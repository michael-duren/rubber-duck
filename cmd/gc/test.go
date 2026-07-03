package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func testCmd(args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: gc test [challenge-slug]")
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

		var cmd *exec.Cmd
		switch meta.Language {
		case "go":
			cmd = exec.Command("go", "test", "./...")
		case "python":
			cmd = exec.Command("python3", "-m", "pytest", "-q")
		default:
			return fmt.Errorf("unsupported language %q", meta.Language)
		}
		cmd.Dir = dir
		out, runErr := cmd.CombinedOutput()

		status := "PASS"
		if runErr != nil {
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
