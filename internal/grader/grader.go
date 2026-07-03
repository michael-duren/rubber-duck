// Package grader defines how challenge submissions are executed and scored.
// Implementations run untrusted code; the interface is the seam where the
// M1 docker-exec runner can be swapped for a hardened sandbox later.
package grader

import (
	"archive/tar"
	"bytes"
	"context"
)

type Job struct {
	Language string
	Code     string
	TestCode string
}

type Result struct {
	Status string // passed | failed | error
	Output string // truncated combined test output

	// TestsPassed/TestsTotal are nil when per-test counts couldn't be
	// parsed from Output (e.g. a panic or timeout cut it short); callers
	// fall back to all-or-nothing scoring on Status in that case.
	TestsPassed *int
	TestsTotal  *int
}

const (
	StatusPassed = "passed"
	StatusFailed = "failed"
	StatusError  = "error"
)

type Grader interface {
	// Grade runs the job's tests against the code. A non-nil error means
	// the grader itself failed; test failures are reported in Result.
	Grade(ctx context.Context, job Job) (Result, error)
}

// LanguageFiles is the file-name convention runner images expect for each
// supported language, shared by every Grader implementation.
var LanguageFiles = map[string]struct {
	Code  string
	Tests string
}{
	"go":     {"solution.go", "solution_test.go"},
	"python": {"solution.py", "test_solution.py"},
}

// Tarball packs files into the tar stream runners consume.
func Tarball(files map[string]string) (*bytes.Buffer, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, content := range files {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return &buf, nil
}

// MaxOutputBytes caps stored test output.
const MaxOutputBytes = 64 << 10

// TruncateOutput trims oversized test output for storage.
func TruncateOutput(s string) string {
	if len(s) > MaxOutputBytes {
		return s[:MaxOutputBytes] + "\n[output truncated]"
	}
	return s
}
