package grader

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/mduren/getcracked/internal/domain"
)

type fakeGrader struct {
	result Result
	err    error
}

func (f fakeGrader) Grade(context.Context, Job) (Result, error) { return f.result, f.err }

type memStore struct {
	mu      sync.Mutex
	jobs    map[int64]domain.SubmissionJob
	done    map[int64]struct {
		status string
		score  int
	}
	audits  map[int64]string
	running map[int64]bool
	graded  chan int64
}

func newMemStore() *memStore {
	return &memStore{
		jobs: map[int64]domain.SubmissionJob{},
		done: map[int64]struct {
			status string
			score  int
		}{},
		audits:  map[int64]string{},
		running: map[int64]bool{},
		graded:  make(chan int64, 16),
	}
}

func (m *memStore) SubmissionJob(_ context.Context, id int64) (domain.SubmissionJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.jobs[id], nil
}

func (m *memStore) MarkSubmissionRunning(_ context.Context, id int64) error {
	m.mu.Lock()
	m.running[id] = true
	m.mu.Unlock()
	return nil
}

func (m *memStore) AuditSubmission(_ context.Context, id int64, status, _ string) error {
	m.mu.Lock()
	m.audits[id] = status
	m.mu.Unlock()
	m.graded <- id
	return nil
}

func (m *memStore) CompleteSubmission(_ context.Context, id int64, status, _ string, score int, _, _ *int) error {
	m.mu.Lock()
	m.done[id] = struct {
		status string
		score  int
	}{status, score}
	m.mu.Unlock()
	m.graded <- id
	return nil
}

func (m *memStore) PendingSubmissionIDs(context.Context) ([]int64, error) { return nil, nil }

func TestPoolScoring(t *testing.T) {
	cases := []struct {
		name       string
		result     Result
		err        error
		wantStatus string
		wantScore  int
	}{
		{"pass earns points", Result{Status: StatusPassed}, nil, StatusPassed, 10},
		{"fail earns nothing", Result{Status: StatusFailed}, nil, StatusFailed, 0},
		{"grader error recorded", Result{}, context.DeadlineExceeded, StatusError, 0},
		{"parsed partial pass earns proportional credit",
			Result{Status: StatusFailed, TestsPassed: intp(3), TestsTotal: intp(4)}, nil, StatusFailed, 8}, // round(10*3/4)=8
		{"parsed all-pass earns full points even via TestsTotal",
			Result{Status: StatusPassed, TestsPassed: intp(4), TestsTotal: intp(4)}, nil, StatusPassed, 10},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := newMemStore()
			store.jobs[1] = domain.SubmissionJob{SubmissionID: 1, Language: "go", Points: 10}
			logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
			pool := NewPool(fakeGrader{c.result, c.err}, store, logger, 1, time.Second)

			pool.Enqueue(1)
			select {
			case <-store.graded:
			case <-time.After(2 * time.Second):
				t.Fatal("submission never graded")
			}
			got := store.done[1]
			if got.status != c.wantStatus || got.score != c.wantScore {
				t.Errorf("got %+v, want status %s score %d", got, c.wantStatus, c.wantScore)
			}
		})
	}
}

// Claimed submissions take the audit path: the run's verdict lands in the
// audit columns, the verdict/score are never rewritten, and the submission
// is never flipped back to "running".
func TestPoolAuditsClaimedSubmissions(t *testing.T) {
	store := newMemStore()
	store.jobs[1] = domain.SubmissionJob{SubmissionID: 1, Language: "go", Points: 10, Claimed: true}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	pool := NewPool(fakeGrader{Result{Status: StatusFailed}, nil}, store, logger, 1, time.Second)

	pool.Enqueue(1)
	select {
	case <-store.graded:
	case <-time.After(2 * time.Second):
		t.Fatal("submission never audited")
	}
	if got := store.audits[1]; got != StatusFailed {
		t.Errorf("audit status = %q, want %q", got, StatusFailed)
	}
	if _, completed := store.done[1]; completed {
		t.Error("audit run must not rewrite the claimed verdict via CompleteSubmission")
	}
	if store.running[1] {
		t.Error("audit run must not mark a claimed submission running")
	}
}

// A grader infra error during an audit saves nothing, leaving the
// submission eligible for a retry at the next Recover.
func TestPoolAuditInfraErrorSavesNothing(t *testing.T) {
	store := newMemStore()
	store.jobs[1] = domain.SubmissionJob{SubmissionID: 1, Language: "go", Points: 10, Claimed: true}
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	pool := NewPool(fakeGrader{Result{}, context.DeadlineExceeded}, store, logger, 1, time.Second)

	pool.Enqueue(1)
	select {
	case <-store.graded:
		t.Fatal("infra-errored audit must not persist a result")
	case <-time.After(200 * time.Millisecond):
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.audits) != 0 || len(store.done) != 0 {
		t.Errorf("expected no persisted results, got audits=%v done=%v", store.audits, store.done)
	}
}
