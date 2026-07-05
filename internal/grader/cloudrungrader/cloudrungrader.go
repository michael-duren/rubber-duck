// Package cloudrungrader grades submissions with Cloud Run Jobs: one
// gVisor-sandboxed job execution per submission, code staged in and results
// staged out through GCS signed URLs.
//
// Protocol with the runner image (see internal/grader/runners/*/run.sh):
// INPUT_URL is a signed GET for a tar of the solution + tests; OUTPUT_URL is
// a signed PUT the runner uploads its result file to. The result's first
// line is the test-command exit code, the rest is combined output, and the
// runner always exits 0 — so a failed *execution* can only mean
// infrastructure trouble, never a failing test.
//
// Tradeoff vs the local docker grader: job containers have network egress
// (they need to reach GCS). gVisor plus a service account with zero IAM
// roles limits the blast radius; signed URLs are the job's only capability.
package cloudrungrader

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	run "cloud.google.com/go/run/apiv2"
	"cloud.google.com/go/storage"

	"github.com/mduren/getcracked/internal/grader"
)

const urlExpiry = 15 * time.Minute

// jobNames maps a course language to its Cloud Run Job.
var jobNames = map[string]string{
	"go":     "gc-grader-go",
	"python": "gc-grader-python",
	"c":      "gc-grader-c",
}

type Config struct {
	Project string
	Region  string
	Bucket  string
}

type Grader struct {
	jobs   jobRunner
	store  objectStore
	logger *slog.Logger
}

func New(ctx context.Context, cfg Config, logger *slog.Logger) (*Grader, error) {
	jc, err := run.NewJobsClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("run client: %w", err)
	}
	sc, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("storage client: %w", err)
	}
	return &Grader{
		jobs:   &runJobs{client: jc, project: cfg.Project, region: cfg.Region},
		store:  &gcsStore{bucket: sc.Bucket(cfg.Bucket)},
		logger: logger,
	}, nil
}

func (g *Grader) Grade(ctx context.Context, job grader.Job) (grader.Result, error) {
	start := time.Now()
	jobName, ok := jobNames[job.Language]
	files, ok2 := grader.LanguageFiles[job.Language]
	if !ok || !ok2 {
		return grader.Result{
			Status: grader.StatusError,
			Output: fmt.Sprintf("no grader for language %q", job.Language),
		}, nil
	}

	id := executionID()
	inKey, outKey := "in/"+id+".tar", "out/"+id+".txt"

	tarBuf, err := grader.Tarball(map[string]string{
		files.Code:  job.Code,
		files.Tests: job.TestCode,
	})
	if err != nil {
		return grader.Result{}, err
	}
	if err := g.store.Put(ctx, inKey, tarBuf.Bytes()); err != nil {
		return grader.Result{}, fmt.Errorf("stage input: %w", err)
	}
	// Objects are cleaned up best-effort with a fresh context (ctx may
	// already be dead); the bucket's 1-day lifecycle rule is the backstop.
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = g.store.Delete(cleanupCtx, inKey)
		_ = g.store.Delete(cleanupCtx, outKey)
	}()
	g.logf(id, "upload", start)

	inURL, err := g.store.SignedGetURL(inKey, urlExpiry)
	if err != nil {
		return grader.Result{}, fmt.Errorf("sign input url: %w", err)
	}
	outURL, err := g.store.SignedPutURL(outKey, "text/plain", urlExpiry)
	if err != nil {
		return grader.Result{}, fmt.Errorf("sign output url: %w", err)
	}

	jobStart := time.Now()
	err = g.jobs.Run(ctx, jobName, map[string]string{
		"INPUT_URL":  inURL,
		"OUTPUT_URL": outURL,
	})
	g.logf(id, "job_run", jobStart)
	if ctx.Err() != nil {
		return grader.Result{Status: grader.StatusError, Output: "[time limit exceeded]"}, nil
	}
	if err != nil {
		return grader.Result{}, err
	}

	fetchStart := time.Now()
	body, err := g.store.Get(ctx, outKey)
	if err != nil {
		return grader.Result{}, fmt.Errorf("read result: %w", err)
	}
	g.logf(id, "result_fetch", fetchStart)
	exitCode, output, err := parseResult(body)
	if err != nil {
		return grader.Result{}, err
	}

	status := grader.StatusPassed
	if exitCode != 0 {
		status = grader.StatusFailed
	}
	truncated := grader.TruncateOutput(output)
	passed, total := grader.ParseTestCounts(job.Language, output)
	if g.logger != nil {
		g.logger.Info("grade complete", "execution", id, "language", job.Language,
			"status", status, "total_ms", time.Since(start).Milliseconds())
	}
	return grader.Result{Status: status, Output: truncated, TestsPassed: passed, TestsTotal: total}, nil
}

func (g *Grader) logf(execID, stage string, since time.Time) {
	if g.logger == nil {
		return
	}
	g.logger.Info("grade stage", "execution", execID, "stage", stage, "ms", time.Since(since).Milliseconds())
}

// parseResult splits the runner's result file: first line is the test exit
// code, the rest is output.
func parseResult(body []byte) (int, string, error) {
	head, rest, _ := strings.Cut(string(body), "\n")
	code, err := strconv.Atoi(strings.TrimSpace(head))
	if err != nil {
		return 0, "", fmt.Errorf("malformed grader result (first line %q)", head)
	}
	return code, rest, nil
}

func executionID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}
