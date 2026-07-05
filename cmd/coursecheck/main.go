// coursecheck validates a course markdown document offline: it runs the
// same ingest parser the API uses, then compile-checks every challenge.
//
// For language "cpp" the convention is: the test file does
// `#include "solution.cpp"` and defines main(), so only the test file is
// compiled. The starter must therefore define the full interface (with
// stub bodies) so the tests compile before the learner writes anything.
//
// Usage:
//
//	go run ./cmd/coursecheck [-solutions dir] courses/foo-cpp.md
//
// With -solutions, any <challenge-slug>.cpp found in dir replaces the
// starter and the tests must then pass (exit 0, no FAIL lines).
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mduren/getcracked/internal/ingest"
)

func main() {
	solDir := flag.String("solutions", "", "directory of <challenge-slug>.cpp reference solutions")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: coursecheck [-solutions dir] <course.md>")
		os.Exit(2)
	}
	src, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	res, err := ingest.Parse(src)
	if err != nil {
		var verr *ingest.ValidationError
		if errors.As(err, &verr) {
			fmt.Printf("INGEST: %d problem(s)\n", len(verr.Problems))
			for _, p := range verr.Problems {
				fmt.Printf("  line %d: %s\n", p.Line, p.Message)
			}
		} else {
			fmt.Println("INGEST:", err)
		}
		os.Exit(1)
	}

	var challenges []ingest.ParsedChallenge
	for _, l := range res.Lessons {
		challenges = append(challenges, l.Challenges...)
	}
	challenges = append(challenges, res.Final)

	total := 0
	for _, c := range challenges {
		total += c.Points
	}
	fmt.Printf("INGEST OK: %d lessons, %d challenges, %d points\n",
		len(res.Lessons), len(challenges), total)

	if res.Course.Language != "cpp" {
		fmt.Println("language is not cpp; skipping compile checks")
		return
	}

	failed := 0
	for _, c := range challenges {
		if err := checkChallenge(c, *solDir); err != nil {
			failed++
			fmt.Printf("FAIL %-40s %v\n", c.Slug, err)
		} else {
			fmt.Printf("ok   %s\n", c.Slug)
		}
	}
	if failed > 0 {
		fmt.Printf("%d/%d challenges failed\n", failed, len(challenges))
		os.Exit(1)
	}
	fmt.Println("all challenges passed")
}

func checkChallenge(c ingest.ParsedChallenge, solDir string) error {
	// Starter must compile against the tests and run to completion
	// (failing tests are expected; crashing is not).
	out, runErr, err := build(c.StarterCode, c.TestCode)
	if err != nil {
		return fmt.Errorf("starter does not compile:\n%s", indent(out))
	}
	if !strings.Contains(out, "--- PASS") && !strings.Contains(out, "--- FAIL") {
		return fmt.Errorf("starter run printed no --- PASS/--- FAIL lines (crash?): %v\n%s", runErr, indent(out))
	}
	if ee := (&exec.ExitError{}); errors.As(runErr, &ee) && ee.ExitCode() == -1 {
		return fmt.Errorf("starter run killed by signal (crash on stubs):\n%s", indent(out))
	}

	if solDir == "" {
		return nil
	}
	sol, err := os.ReadFile(filepath.Join(solDir, c.Slug+".cpp"))
	if err != nil {
		return fmt.Errorf("no reference solution: %v", err)
	}
	out, runErr, err = build(string(sol), c.TestCode)
	if err != nil {
		return fmt.Errorf("solution does not compile:\n%s", indent(out))
	}
	if runErr != nil || strings.Contains(out, "--- FAIL") {
		return fmt.Errorf("solution does not pass tests (%v):\n%s", runErr, indent(out))
	}
	return nil
}

// build writes solution.cpp + test_solution.cpp, compiles the test file
// (which #includes the solution), and runs it. Returns combined
// compiler-or-program output, the run error, and a compile error.
func build(solution, tests string) (string, error, error) {
	dir, err := os.MkdirTemp("", "coursecheck")
	if err != nil {
		return "", nil, err
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "solution.cpp"), []byte(solution), 0o644); err != nil {
		return "", nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "test_solution.cpp"), []byte(tests), 0o644); err != nil {
		return "", nil, err
	}
	bin := filepath.Join(dir, "test_bin")
	cc := exec.Command("g++", "-std=c++20", "-Wall", "-Wextra", "-O1", "-fsanitize=address,undefined",
		"-o", bin, "test_solution.cpp", "-pthread")
	cc.Dir = dir
	if out, err := cc.CombinedOutput(); err != nil {
		return string(out), nil, fmt.Errorf("g++: %w", err)
	}

	run := exec.Command(bin)
	run.Dir = dir
	run.WaitDelay = time.Second
	done := make(chan struct{})
	var out []byte
	var runErr error
	go func() {
		out, runErr = run.CombinedOutput()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		if run.Process != nil {
			_ = run.Process.Kill()
		}
		<-done
		return string(out), fmt.Errorf("timeout after 30s"), nil
	}
	return string(out), runErr, nil
}

func indent(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > 40 {
		lines = append(lines[:40], fmt.Sprintf("... (%d more lines)", len(lines)-40))
	}
	return "    " + strings.Join(lines, "\n    ")
}
