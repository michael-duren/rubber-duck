// Package dockergrader grades submissions by running `docker run` against
// per-language runner images (gc-runner-go, gc-runner-python). Containers get
// no network and capped memory/CPU/pids; the container filesystem is
// discarded after each run (--rm).
//
// Code reaches the container as a tar stream on stdin rather than a bind
// mount, so grading works the same whether the app runs on the host or in a
// container that only shares the host's docker socket.
package dockergrader

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"

	"github.com/mduren/getcracked/internal/grader"
)

// images maps a course language to its local runner image; file names come
// from grader.LanguageFiles.
var images = map[string]string{
	"go":     "gc-runner-go",
	"python": "gc-runner-python",
}

type Grader struct{}

func New() *Grader { return &Grader{} }

func (g *Grader) Grade(ctx context.Context, job grader.Job) (grader.Result, error) {
	image, ok := images[job.Language]
	files, ok2 := grader.LanguageFiles[job.Language]
	if !ok || !ok2 {
		return grader.Result{
			Status: grader.StatusError,
			Output: fmt.Sprintf("no grader for language %q", job.Language),
		}, nil
	}

	stdin, err := grader.Tarball(map[string]string{
		files.Code:  job.Code,
		files.Tests: job.TestCode,
	})
	if err != nil {
		return grader.Result{}, err
	}

	// Name the container so it can be force-removed: on timeout,
	// CommandContext kills only the docker *client*; the container itself
	// belongs to the daemon and would keep running (and burning CPU).
	name := containerName()
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "-i",
		"--name", name,
		"--network=none",
		"--memory=256m",
		"--cpus=1",
		"--pids-limit=128",
		image,
	)
	cmd.Stdin = stdin
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err = cmd.Run()
	// After a normal run --rm has already removed it; this only matters
	// when the client died before the container did. Errors are expected.
	defer func() { _ = exec.Command("docker", "rm", "-f", name).Run() }()
	output := grader.TruncateOutput(out.String())

	switch {
	case err == nil:
		return grader.Result{Status: grader.StatusPassed, Output: output}, nil
	case ctx.Err() != nil:
		return grader.Result{Status: grader.StatusError, Output: output + "\n[time limit exceeded]"}, nil
	default:
		if _, ok := errors.AsType[*exec.ExitError](err); ok {
			return grader.Result{Status: grader.StatusFailed, Output: output}, nil
		}
		// docker missing, image missing, daemon down: infra failure.
		return grader.Result{}, fmt.Errorf("run grader container: %w", err)
	}
}

func containerName() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "gc-grade-" + hex.EncodeToString(b)
}
