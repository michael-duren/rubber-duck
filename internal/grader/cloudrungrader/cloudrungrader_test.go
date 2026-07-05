package cloudrungrader

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/michael-duren/rubber-duck/internal/grader"
)

type fakeJobs struct {
	err     error
	slow    time.Duration
	gotJob  string
	gotEnv  map[string]string
	writeFn func(env map[string]string) // simulates the runner uploading
}

func (f *fakeJobs) Run(ctx context.Context, jobName string, env map[string]string) error {
	f.gotJob, f.gotEnv = jobName, env
	if f.slow > 0 {
		select {
		case <-time.After(f.slow):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if f.writeFn != nil {
		f.writeFn(env)
	}
	return f.err
}

type fakeStore struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newFakeStore() *fakeStore { return &fakeStore{objects: map[string][]byte{}} }

func (f *fakeStore) Put(_ context.Context, key string, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[key] = data
	return nil
}

func (f *fakeStore) Get(_ context.Context, key string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.objects[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return b, nil
}

func (f *fakeStore) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objects, key)
	return nil
}

func (f *fakeStore) SignedGetURL(key string, _ time.Duration) (string, error) {
	return "https://signed.example/GET/" + key, nil
}

func (f *fakeStore) SignedPutURL(key, _ string, _ time.Duration) (string, error) {
	return "https://signed.example/PUT/" + key, nil
}

// outKeyFromEnv extracts the output object key from the signed PUT URL the
// grader handed to the job.
func outKeyFromEnv(env map[string]string) string {
	return strings.TrimPrefix(env["OUTPUT_URL"], "https://signed.example/PUT/")
}

func testJob() grader.Job {
	return grader.Job{Language: "go", Code: "package x", TestCode: "package x_test"}
}

func TestGrade(t *testing.T) {
	cases := []struct {
		name       string
		result     string // runner output file content; "" = runner never uploads
		jobErr     error
		wantStatus string
		wantOutput string
		wantErr    bool
	}{
		{name: "exit 0 passes", result: "0\nok  \tchallenge\t0.01s\n", wantStatus: grader.StatusPassed, wantOutput: "ok"},
		{name: "nonzero exit fails", result: "1\n--- FAIL: TestX\n", wantStatus: grader.StatusFailed, wantOutput: "FAIL"},
		{name: "malformed result is infra error", result: "not-a-number\nstuff", wantErr: true},
		{name: "execution failure is infra error", result: "", jobErr: errors.New("task exceeded retries"), wantErr: true},
		{name: "missing output after success is infra error", result: "", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := newFakeStore()
			jobs := &fakeJobs{err: c.jobErr}
			if c.result != "" {
				jobs.writeFn = func(env map[string]string) {
					store.Put(context.Background(), outKeyFromEnv(env), []byte(c.result))
				}
			}
			g := &Grader{jobs: jobs, store: store}

			res, err := g.Grade(context.Background(), testJob())
			if c.wantErr {
				if err == nil {
					t.Fatalf("err = nil, res = %+v; want error", res)
				}
				return
			}
			if err != nil {
				t.Fatalf("Grade: %v", err)
			}
			if res.Status != c.wantStatus || !strings.Contains(res.Output, c.wantOutput) {
				t.Errorf("res = %+v, want status %s containing %q", res, c.wantStatus, c.wantOutput)
			}
			if jobs.gotJob != "gc-grader-go" {
				t.Errorf("job = %q", jobs.gotJob)
			}
			if !strings.HasPrefix(jobs.gotEnv["INPUT_URL"], "https://signed.example/GET/in/") {
				t.Errorf("INPUT_URL = %q", jobs.gotEnv["INPUT_URL"])
			}
			if len(store.objects) != 0 {
				t.Errorf("objects not cleaned up: %v", store.objects)
			}
		})
	}
}

func TestGradeTimeout(t *testing.T) {
	store := newFakeStore()
	jobs := &fakeJobs{slow: time.Second}
	g := &Grader{jobs: jobs, store: store}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	res, err := g.Grade(ctx, testJob())
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if res.Status != grader.StatusError || !strings.Contains(res.Output, "time limit") {
		t.Errorf("res = %+v, want error status with time limit message", res)
	}
}

func TestGradeUnknownLanguage(t *testing.T) {
	g := &Grader{jobs: &fakeJobs{}, store: newFakeStore()}
	res, err := g.Grade(context.Background(), grader.Job{Language: "cobol"})
	if err != nil || res.Status != grader.StatusError {
		t.Errorf("res = %+v, err = %v", res, err)
	}
}

func TestParseResult(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantCode int
		wantOut  string
		wantErr  bool
	}{
		{"pass", "0\nok\n", 0, "ok\n", false},
		{"fail with output", "2\nFAIL: x\nmore\n", 2, "FAIL: x\nmore\n", false},
		{"trailing space on code", "1 \noutput", 1, "output", false},
		{"no output", "0", 0, "", false},
		{"empty", "", 0, "", true},
		{"garbage", "hello\nworld", 0, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, out, err := parseResult([]byte(c.body))
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if !c.wantErr && (code != c.wantCode || out != c.wantOut) {
				t.Errorf("= %d, %q; want %d, %q", code, out, c.wantCode, c.wantOut)
			}
		})
	}
}
