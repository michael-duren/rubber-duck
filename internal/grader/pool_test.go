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
	graded chan int64
}

func newMemStore() *memStore {
	return &memStore{
		jobs: map[int64]domain.SubmissionJob{},
		done: map[int64]struct {
			status string
			score  int
		}{},
		graded: make(chan int64, 16),
	}
}

func (m *memStore) SubmissionJob(_ context.Context, id int64) (domain.SubmissionJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.jobs[id], nil
}

func (m *memStore) MarkSubmissionRunning(context.Context, int64) error { return nil }

func (m *memStore) CompleteSubmission(_ context.Context, id int64, status, _ string, score int) error {
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
