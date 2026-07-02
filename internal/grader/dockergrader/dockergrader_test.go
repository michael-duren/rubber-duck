package dockergrader

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/mduren/getcracked/internal/grader"
)

const testCode = `package challenge

import "testing"

func TestDouble(t *testing.T) {
	if got := Double(2); got != 4 {
		t.Errorf("Double(2) = %d, want 4", got)
	}
}
`

// requireDocker skips unless the docker daemon and runner image are ready.
func requireDocker(t *testing.T) {
	t.Helper()
	if err := exec.Command("docker", "image", "inspect", "gc-runner-go").Run(); err != nil {
		t.Skip("docker or gc-runner-go image unavailable; run `make runner-images`")
	}
}

func TestGrade(t *testing.T) {
	requireDocker(t)
	g := New()

	cases := []struct {
		name       string
		code       string
		timeout    time.Duration
		wantStatus string
		wantOutput string
	}{
		{
			"correct solution passes",
			"package challenge\n\nfunc Double(n int) int { return n * 2 }\n",
			60 * time.Second,
			grader.StatusPassed, "ok",
		},
		{
			"wrong solution fails",
			"package challenge\n\nfunc Double(n int) int { return n * 3 }\n",
			60 * time.Second,
			grader.StatusFailed, "Double(2) = 6",
		},
		{
			"infinite loop times out",
			"package challenge\n\nfunc Double(n int) int { for {} }\n",
			25 * time.Second,
			grader.StatusError, "time limit",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
			defer cancel()
			res, err := g.Grade(ctx, grader.Job{Language: "go", Code: c.code, TestCode: testCode})
			if err != nil {
				t.Fatalf("Grade: %v", err)
			}
			if res.Status != c.wantStatus {
				t.Errorf("status = %s, want %s (output: %s)", res.Status, c.wantStatus, res.Output)
			}
			if !strings.Contains(res.Output, c.wantOutput) {
				t.Errorf("output %q missing %q", res.Output, c.wantOutput)
			}
			// No grading container may outlive Grade — a killed docker
			// client must not leak its container.
			out, err := exec.Command("docker", "ps", "--filter", "name=gc-grade-", "-q").Output()
			if err != nil {
				t.Fatalf("docker ps: %v", err)
			}
			if got := strings.TrimSpace(string(out)); got != "" {
				t.Errorf("grading container leaked: %s", got)
			}
		})
	}
}

func TestGradeUnknownLanguage(t *testing.T) {
	g := New()
	res, err := g.Grade(context.Background(), grader.Job{Language: "cobol"})
	if err != nil || res.Status != grader.StatusError {
		t.Errorf("res = %+v, err = %v", res, err)
	}
}
