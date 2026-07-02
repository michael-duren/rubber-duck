// Package grader defines how challenge submissions are executed and scored.
// Implementations run untrusted code; the interface is the seam where the
// M1 docker-exec runner can be swapped for a hardened sandbox later.
package grader

import "context"

type Job struct {
	Language string
	Code     string
	TestCode string
}

type Result struct {
	Status string // passed | failed | error
	Output string // truncated combined test output
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
