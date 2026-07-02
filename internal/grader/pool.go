package grader

import (
	"context"
	"log/slog"
	"time"

	"github.com/mduren/getcracked/internal/domain"
)

// SubmissionStore is the persistence the pool needs around a grading run.
type SubmissionStore interface {
	SubmissionJob(ctx context.Context, id int64) (domain.SubmissionJob, error)
	MarkSubmissionRunning(ctx context.Context, id int64) error
	CompleteSubmission(ctx context.Context, id int64, status, output string, score int) error
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
	if err := p.store.MarkSubmissionRunning(ctx, id); err != nil {
		p.logger.Error("mark running", "id", id, "err", err)
		return
	}

	res, err := p.grader.Grade(ctx, Job{Language: job.Language, Code: job.Code, TestCode: job.TestCode})
	if err != nil {
		p.logger.Error("grade", "id", id, "err", err)
		res = Result{Status: StatusError, Output: "grader unavailable; try again later"}
	}
	score := 0
	if res.Status == StatusPassed {
		score = job.Points
	}

	// Persist with a fresh context: the job context may have timed out.
	saveCtx, saveCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer saveCancel()
	if err := p.store.CompleteSubmission(saveCtx, id, res.Status, res.Output, score); err != nil {
		p.logger.Error("save result", "id", id, "err", err)
	}
}
