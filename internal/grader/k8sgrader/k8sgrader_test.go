package k8sgrader

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/michael-duren/rubber-duck/internal/grader"
)

// fakeAPI is just enough of the core v1 API: it records creates and
// deletes and serves a scripted pod status + log.
type fakeAPI struct {
	mu        sync.Mutex
	created   []string // "pods/name", "configmaps/name"
	deleted   []string
	podJSON   string // response to GET pods/<name>
	logOutput string
	podBody   map[string]any // captured pod create payload
}

func (f *fakeAPI) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing bearer token on %s %s", r.Method, r.URL.Path)
		}
		rest := strings.TrimPrefix(r.URL.Path, "/api/v1/namespaces/duck/")
		switch {
		case r.Method == http.MethodPost:
			if strings.HasPrefix(rest, "pods") {
				_ = json.NewDecoder(r.Body).Decode(&f.podBody)
			}
			f.created = append(f.created, rest)
			_, _ = fmt.Fprint(w, `{}`)
		case r.Method == http.MethodDelete:
			f.deleted = append(f.deleted, strings.SplitN(rest, "?", 2)[0])
			_, _ = fmt.Fprint(w, `{}`)
		case strings.HasSuffix(rest, "/log"):
			_, _ = fmt.Fprint(w, f.logOutput)
		default: // GET pod
			_, _ = fmt.Fprint(w, f.podJSON)
		}
	})
}

func testGrader(t *testing.T, f *fakeAPI) (*Grader, *httptest.Server) {
	srv := httptest.NewServer(f.handler(t))
	g := New(Config{
		APIServer: srv.URL,
		Namespace: "duck",
		Token:     func() (string, error) { return "test-token", nil },
		Client:    srv.Client(),
	}, slog.New(slog.NewTextHandler(&strings.Builder{}, nil)))
	return g, srv
}

func terminated(exitCode int) string {
	return fmt.Sprintf(`{"status":{"phase":"Running","containerStatuses":[{"state":{"terminated":{"exitCode":%d}}}]}}`, exitCode)
}

func TestGrade(t *testing.T) {
	goOut := "=== RUN   TestSum\n--- PASS: TestSum (0.00s)\nPASS\nok  \tchallenge\t0.01s\n"
	tests := []struct {
		name       string
		podJSON    string
		logOutput  string
		wantStatus string
		wantInfra  bool // non-nil error from Grade
	}{
		{
			name:       "passing run",
			podJSON:    terminated(0),
			logOutput:  goOut,
			wantStatus: grader.StatusPassed,
		},
		{
			name:       "failing tests",
			podJSON:    terminated(1),
			logOutput:  "--- FAIL: TestSum (0.00s)\nFAIL\n",
			wantStatus: grader.StatusFailed,
		},
		{
			name:      "runner image missing is infra failure",
			podJSON:   `{"status":{"phase":"Pending","containerStatuses":[{"state":{"waiting":{"reason":"ErrImageNeverPull"}}}]}}`,
			wantInfra: true,
		},
		{
			name:      "pod failed before container ran",
			podJSON:   `{"status":{"phase":"Failed","reason":"DeadlineExceeded"}}`,
			wantInfra: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeAPI{podJSON: tt.podJSON, logOutput: tt.logOutput}
			g, srv := testGrader(t, f)
			defer srv.Close()

			res, err := g.Grade(context.Background(), grader.Job{Language: "go", Code: "c", TestCode: "t"})
			if tt.wantInfra {
				if err == nil {
					t.Fatalf("want infra error, got result %+v", res)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if res.Status != tt.wantStatus {
					t.Errorf("status = %q, want %q", res.Status, tt.wantStatus)
				}
				if !strings.Contains(res.Output, strings.TrimSpace(tt.logOutput[:4])) {
					t.Errorf("output %q missing pod log", res.Output)
				}
			}

			// Pod and configmap must be cleaned up on every path.
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.created) != 2 || len(f.deleted) != 2 {
				t.Errorf("created %v deleted %v, want one pod + one configmap each", f.created, f.deleted)
			}
		})
	}
}

func TestGradePassedParsesCounts(t *testing.T) {
	f := &fakeAPI{
		podJSON:   terminated(0),
		logOutput: "=== RUN   TestA\n--- PASS: TestA (0.00s)\n=== RUN   TestB\n--- PASS: TestB (0.00s)\nPASS\nok  \tchallenge\t0.01s\n",
	}
	g, srv := testGrader(t, f)
	defer srv.Close()

	res, err := g.Grade(context.Background(), grader.Job{Language: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if res.TestsPassed == nil || res.TestsTotal == nil || *res.TestsPassed != 2 || *res.TestsTotal != 2 {
		t.Errorf("counts = %v/%v, want 2/2", res.TestsPassed, res.TestsTotal)
	}
}

func TestGradeTimeout(t *testing.T) {
	// Pod never terminates; the context expires first.
	f := &fakeAPI{podJSON: `{"status":{"phase":"Running","containerStatuses":[{"state":{}}]}}`}
	g, srv := testGrader(t, f)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	res, err := g.Grade(ctx, grader.Job{Language: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != grader.StatusError || !strings.Contains(res.Output, "time limit") {
		t.Errorf("got %+v, want error status with time limit message", res)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.deleted) != 2 {
		t.Errorf("timed-out pod not cleaned up: deleted %v", f.deleted)
	}
}

func TestGradeUnknownLanguage(t *testing.T) {
	g, srv := testGrader(t, &fakeAPI{})
	defer srv.Close()
	res, err := g.Grade(context.Background(), grader.Job{Language: "cobol"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != grader.StatusError {
		t.Errorf("status = %q, want error", res.Status)
	}
}

func TestPodSpecSandbox(t *testing.T) {
	f := &fakeAPI{podJSON: terminated(0)}
	g, srv := testGrader(t, f)
	defer srv.Close()
	if _, err := g.Grade(context.Background(), grader.Job{Language: "go"}); err != nil {
		t.Fatal(err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	spec := f.podBody["spec"].(map[string]any)
	if spec["automountServiceAccountToken"] != false {
		t.Error("grading pod must not get a service account token")
	}
	if spec["activeDeadlineSeconds"] == nil {
		t.Error("grading pod must have a server-side deadline")
	}
	c := spec["containers"].([]any)[0].(map[string]any)
	if c["imagePullPolicy"] != "Never" {
		t.Error("runner images are imported, never pulled")
	}
	cmd := c["command"].([]any)[2].(string)
	if !strings.Contains(cmd, "solution.go solution_test.go | /run.sh") {
		t.Errorf("pod command %q does not pipe the job tar into /run.sh", cmd)
	}

	// Grading writes must stay in RAM: node disks with slow flushes turned
	// each run's journal commits into tens of seconds of dead time.
	volumes := spec["volumes"].([]any)
	foundTmpfs := false
	for _, v := range volumes {
		if ed, ok := v.(map[string]any)["emptyDir"].(map[string]any); ok && ed["medium"] == "Memory" {
			foundTmpfs = true
		}
	}
	if !foundTmpfs {
		t.Error("grading pod must mount a Memory-backed emptyDir for its writes")
	}
	if !strings.Contains(cmd, "cp -r /gocache /tmp/gocache") {
		t.Errorf("pod command %q does not stage the warm build cache into tmpfs", cmd)
	}
}
