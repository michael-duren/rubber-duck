package grader

import (
	"context"
	"log/slog"
	"math"
	"time"

	"github.com/michael-duren/rubber-duck/internal/domain"
)

// SubmissionStore is the persistence the pool needs around a grading run.
type SubmissionStore interface {
	SubmissionJob(ctx context.Context, id int64) (domain.SubmissionJob, error)
	MarkSubmissionRunning(ctx context.Context, id int64) error
	CompleteSubmission(ctx context.Context, id int64, status, output string, score int, testsPassed, testsTotal *int) error
	AuditSubmission(ctx context.Context, id int64, status, output string) error
	PendingSubmissionIDs(ctx context.Context) ([]int64, error)
}

// Pool grades submissions on a fixed set of workers. Enqueue never blocks
// the caller: the queue is buffered and overflow falls back to a goroutine.
type Pool struct {
	grader  Grader
	store   SubmissionStore
	logger  *slog.Logger
	queue   chan int64
	timeout time.Duration
}

func NewPool(g Grader, store SubmissionStore, logger *slog.Logger, workers int, timeout time.Duration) *Pool {
	p := &Pool{
		grader:  g,
		store:   store,
		logger:  logger,
		queue:   make(chan int64, 256),
		timeout: timeout,
	}
	for range workers {
		go p.work()
	}
	return p
}

// Enqueue schedules a submission for grading.
func (p *Pool) Enqueue(id int64) {
	select {
	case p.queue <- id:
	default:
		go func() { p.queue <- id }()
	}
}

// Recover re-enqueues submissions that were pending when the process
// stopped. Call once on startup.
func (p *Pool) Recover(ctx context.Context) error {
	ids, err := p.store.PendingSubmissionIDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		p.Enqueue(id)
	}
	if len(ids) > 0 {
		p.logger.Info("requeued pending submissions", "count", len(ids))
	}
	return nil
}

func (p *Pool) work() {
	for id := range p.queue {
		p.grade(id)
	}
}

func (p *Pool) grade(id int64) {
	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()

	job, err := p.store.SubmissionJob(ctx, id)
	if err != nil {
		p.logger.Error("load submission job", "id", id, "err", err)
		return
	}
	if !job.Claimed {
		if err := p.store.MarkSubmissionRunning(ctx, id); err != nil {
			p.logger.Error("mark running", "id", id, "err", err)
			return
		}
	}

	res, err := p.grader.Grade(ctx, Job{Language: job.Language, Code: job.Code, TestCode: job.TestCode})

	// Persist with a fresh context: the job context may have timed out.
	saveCtx, saveCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer saveCancel()

	if job.Claimed {
		// Audit of a CLI-claimed verdict. On grader infra failure, save
		// nothing: the submission stays unaudited and Recover retries it
		// on the next startup rather than recording a verdict-less audit.
		if err != nil {
			p.logger.Error("audit grade", "id", id, "err", err)
			return
		}
		if err := p.store.AuditSubmission(saveCtx, id, res.Status, res.Output); err != nil {
			p.logger.Error("save audit", "id", id, "err", err)
		}
		return
	}

	if err != nil {
		p.logger.Error("grade", "id", id, "err", err)
		res = Result{Status: StatusError, Output: "grader unavailable; try again later"}
	}
	score := Score(job.Points, res)
	if err := p.store.CompleteSubmission(saveCtx, id, res.Status, res.Output, score, res.TestsPassed, res.TestsTotal); err != nil {
		p.logger.Error("save result", "id", id, "err", err)
	}
}

// Score is proportional to tests passed when they could be parsed out of
// the runner's output (full points only on an all-pass run), falling back
// to all-or-nothing on the exit status when they couldn't.
func Score(points int, res Result) int {
	if res.TestsTotal != nil && *res.TestsTotal > 0 {
		return int(math.Round(float64(points) * float64(*res.TestsPassed) / float64(*res.TestsTotal)))
	}
	if res.Status == StatusPassed {
		return points
	}
	return 0
}
