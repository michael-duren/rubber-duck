// coursecheck validates a course markdown document offline: it runs the
// same ingest parser the API uses, then compile-checks every challenge
// (languages "cpp" and "c"; others parse-check only).
//
// For language "cpp" the convention is: the test file does
// `#include "solution.cpp"` and defines main(), so only the test file is
// compiled. For "c", solution.c and test_solution.c are separate
// translation units compiled together, exactly like the grader runner and
// `duck test`. Either way the starter must define the full interface (with
// stub bodies) so the tests compile before the learner writes anything.
//
// Usage:
//
//	go run ./cmd/coursecheck [-solutions dir] courses/foo-cpp.md
//
// With -solutions, any <challenge-slug>.<ext> found in dir replaces the
// starter and the tests must then pass (exit 0, no FAIL lines).
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/michael-duren/rubber-duck/internal/ingest"
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
		if verr, ok := errors.AsType[*ingest.ValidationError](err); ok {
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

	lang := res.Course.Language
	if lang != "cpp" && lang != "c" {
		fmt.Printf("language %q has no compile checks; done\n", lang)
		return
	}

	failed := 0
	for _, c := range challenges {
		if err := checkChallenge(lang, c, *solDir); err != nil {
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

func checkChallenge(lang string, c ingest.ParsedChallenge, solDir string) error {
	// Starter must compile against the tests and run to completion
	// (failing tests are expected; crashing is not).
	out, runErr, err := build(lang, c.StarterCode, c.TestCode)
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
	sol, err := os.ReadFile(filepath.Join(solDir, c.Slug+"."+lang))
	if err != nil {
		return fmt.Errorf("no reference solution: %v", err)
	}
	out, runErr, err = build(lang, string(sol), c.TestCode)
	if err != nil {
		return fmt.Errorf("solution does not compile:\n%s", indent(out))
	}
	if runErr != nil || strings.Contains(out, "--- FAIL") {
		return fmt.Errorf("solution does not pass tests (%v):\n%s", runErr, indent(out))
	}
	return nil
}

// build writes solution + tests under the language's grader file names,
// compiles them the way that language's runner does (plus sanitizers,
// which the runner lacks — coursecheck is the author's gate, so stricter
// is better), and runs the binary. Returns combined compiler-or-program
// output, the run error, and a compile error.
func build(lang, solution, tests string) (string, error, error) {
	dir, err := os.MkdirTemp("", "coursecheck")
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := os.WriteFile(filepath.Join(dir, "solution."+lang), []byte(solution), 0o644); err != nil {
		return "", nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "test_solution."+lang), []byte(tests), 0o644); err != nil {
		return "", nil, err
	}
	bin := filepath.Join(dir, "test_bin")
	var cc *exec.Cmd
	switch lang {
	case "cpp":
		// The test file #includes solution.cpp, so only it is compiled.
		cc = exec.Command("g++", "-std=c++20", "-Wall", "-Wextra", "-O1", "-fsanitize=address,undefined",
			"-o", bin, "test_solution.cpp", "-pthread")
	case "c":
		// Two translation units, mirroring gc-runner-c and `duck test`.
		cc = exec.Command("cc", "-std=c17", "-Wall", "-Wextra", "-O1", "-fsanitize=address,undefined",
			"-o", bin, "solution.c", "test_solution.c")
	default:
		return "", nil, fmt.Errorf("no build rule for language %q", lang)
	}
	cc.Dir = dir
	if out, err := cc.CombinedOutput(); err != nil {
		return string(out), nil, fmt.Errorf("%s: %w", cc.Path, err)
	}

	run := exec.Command(bin)
	run.Dir = dir
	// The graders build without ASan, where an impossibly large malloc
	// just returns NULL; ASan's default is to abort instead. Courses test
	// allocation-failure paths that way, so keep NULL-return semantics.
	// Drop any inherited ASAN_OPTIONS first: getenv returns the first
	// match in environ, so a duplicate would shadow ours.
	env := slices.DeleteFunc(os.Environ(), func(v string) bool {
		return strings.HasPrefix(v, "ASAN_OPTIONS=")
	})
	run.Env = append(env, "ASAN_OPTIONS=allocator_may_return_null=1")
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
