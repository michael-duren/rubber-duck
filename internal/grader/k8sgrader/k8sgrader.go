// Package k8sgrader grades submissions as Kubernetes pods: one pod per
// submission, running the same gc-runner-* images as the docker grader, on
// the cluster's own container runtime. No docker daemon or socket mount is
// involved — this is the containerd-native path for cluster deployments.
//
// Protocol with the runner image (see internal/grader/runners/*/run.sh):
// the solution + tests land in a ConfigMap mounted at /job, and the pod
// command tars them into /run.sh's stdin — byte-for-byte the local-docker
// contract, so the runner images don't grow a third input mode. The test
// exit code is the container exit code and combined output is the pod log.
//
// Grading pods get no service account token and, when the cluster enforces
// NetworkPolicy (k3s does), no network — see deploy/homelab/rbac.yaml and
// networkpolicy.yaml. The kube API is spoken with net/http directly: the
// three verbs needed (create, get, delete) don't justify a client-go
// dependency in a stdlib-first repo.
package k8sgrader

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/michael-duren/rubber-duck/internal/grader"
)

// images maps a course language to its runner image, which must already be
// present in the node's container runtime (imagePullPolicy Never).
var images = map[string]string{
	"go":     "gc-runner-go",
	"python": "gc-runner-python",
	"c":      "gc-runner-c",
}

// activeDeadlineSeconds is the server-side per-pod time budget, mirroring
// the runners' 90s task limit on Cloud Run. The client-side context is
// larger (scheduling and container start happen first); this backstop kills
// the pod even if this process dies mid-grade.
const activeDeadlineSeconds = 90

// maxLogBytes caps how much pod output is fetched: untrusted code can print
// without bound, and limitBytes makes the API server do the truncating.
const maxLogBytes = 1 << 20

type Config struct {
	// APIServer is the base URL of the Kubernetes API, e.g. https://10.43.0.1:443.
	APIServer string
	// Namespace to create grading pods in.
	Namespace string
	// Token returns a bearer token for the API. It is called per request:
	// in-cluster service account tokens are short-lived and the kubelet
	// refreshes the mounted file, so caching one at startup would expire.
	Token func() (string, error)
	// Client is the HTTP client to use; it must trust the cluster CA.
	Client *http.Client
}

// InCluster builds a Config from the standard in-cluster environment
// (KUBERNETES_SERVICE_HOST/PORT + the mounted service account).
func InCluster() (Config, error) {
	const saDir = "/var/run/secrets/kubernetes.io/serviceaccount"
	host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return Config{}, fmt.Errorf("not running in a cluster: KUBERNETES_SERVICE_HOST/PORT unset")
	}
	ns, err := os.ReadFile(saDir + "/namespace")
	if err != nil {
		return Config{}, fmt.Errorf("read namespace: %w", err)
	}
	caPEM, err := os.ReadFile(saDir + "/ca.crt")
	if err != nil {
		return Config{}, fmt.Errorf("read cluster CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return Config{}, fmt.Errorf("cluster CA %s/ca.crt contains no certificates", saDir)
	}
	return Config{
		APIServer: "https://" + host + ":" + port,
		Namespace: strings.TrimSpace(string(ns)),
		Token: func() (string, error) {
			b, err := os.ReadFile(saDir + "/token")
			if err != nil {
				return "", fmt.Errorf("read service account token: %w", err)
			}
			return strings.TrimSpace(string(b)), nil
		},
		Client: &http.Client{
			Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}},
		},
	}, nil
}

type Grader struct {
	cfg    Config
	logger *slog.Logger
}

func New(cfg Config, logger *slog.Logger) *Grader {
	return &Grader{cfg: cfg, logger: logger}
}

func (g *Grader) Grade(ctx context.Context, job grader.Job) (grader.Result, error) {
	image, ok := images[job.Language]
	files, ok2 := grader.LanguageFiles[job.Language]
	if !ok || !ok2 {
		return grader.Result{
			Status: grader.StatusError,
			Output: fmt.Sprintf("no grader for language %q", job.Language),
		}, nil
	}

	name := podName()

	if err := g.createConfigMap(ctx, name, map[string]string{
		files.Code:  job.Code,
		files.Tests: job.TestCode,
	}); err != nil {
		return grader.Result{}, fmt.Errorf("create job configmap: %w", err)
	}
	defer g.delete("configmaps", name)

	if err := g.createPod(ctx, name, image, files.Code, files.Tests); err != nil {
		return grader.Result{}, fmt.Errorf("create grading pod: %w", err)
	}
	defer g.delete("pods", name)

	exitCode, infraErr, err := g.awaitPod(ctx, name)
	if err != nil {
		if ctx.Err() != nil {
			return grader.Result{Status: grader.StatusError, Output: "[time limit exceeded]"}, nil
		}
		return grader.Result{}, err
	}
	if infraErr != "" {
		return grader.Result{}, fmt.Errorf("grading pod %s: %s", name, infraErr)
	}

	raw := g.podLogs(ctx, name)
	output := grader.TruncateOutput(raw)
	passed, total := grader.ParseTestCounts(job.Language, raw)

	if exitCode == 0 {
		return grader.Result{Status: grader.StatusPassed, Output: output, TestsPassed: passed, TestsTotal: total}, nil
	}
	return grader.Result{Status: grader.StatusFailed, Output: output, TestsPassed: passed, TestsTotal: total}, nil
}

// podStatus is the slice of the Pod object this grader reads back.
type podStatus struct {
	Status struct {
		Phase             string `json:"phase"`
		Reason            string `json:"reason"`
		ContainerStatuses []struct {
			State struct {
				Waiting *struct {
					Reason string `json:"reason"`
				} `json:"waiting"`
				Terminated *struct {
					ExitCode int    `json:"exitCode"`
					Reason   string `json:"reason"`
				} `json:"terminated"`
			} `json:"state"`
		} `json:"containerStatuses"`
	} `json:"status"`
}

// awaitPod polls until the grading container terminates. It returns the
// container exit code, or a non-empty infra description when the pod ended
// without the container ever running (image missing, deadline exceeded,
// eviction) — failures of the grader, not of the submission.
func (g *Grader) awaitPod(ctx context.Context, name string) (exitCode int, infra string, err error) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		var pod podStatus
		if err := g.api(ctx, http.MethodGet, "pods/"+name, nil, &pod); err != nil {
			return 0, "", err
		}
		if cs := pod.Status.ContainerStatuses; len(cs) > 0 {
			if t := cs[0].State.Terminated; t != nil {
				return t.ExitCode, "", nil
			}
			// A pod that can never start would otherwise sit Pending
			// until the deadline; fail fast with the real reason.
			if w := cs[0].State.Waiting; w != nil &&
				(w.Reason == "ErrImageNeverPull" || w.Reason == "ImagePullBackOff") {
				return 0, "runner image not present on node: " + w.Reason, nil
			}
		}
		if pod.Status.Phase == "Failed" {
			reason := pod.Status.Reason
			if reason == "" {
				reason = "pod failed before the container ran"
			}
			return 0, reason, nil
		}
		select {
		case <-ctx.Done():
			return 0, "", ctx.Err()
		case <-tick.C:
		}
	}
}

func (g *Grader) createConfigMap(ctx context.Context, name string, data map[string]string) error {
	return g.api(ctx, http.MethodPost, "configmaps", map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": name, "labels": podLabels},
		"data":       data,
	}, nil)
}

// podLabels lets RBAC-adjacent policy (the deny-all NetworkPolicy) select
// grading pods without catching anything else in the namespace.
var podLabels = map[string]string{"app": "gc-grade"}

func (g *Grader) createPod(ctx context.Context, name, image, codeFile, testFile string) error {
	// -h dereferences the configmap volume's symlink farm so the tar the
	// runner unpacks holds regular files, exactly like the docker path.
	//
	// Everything a grading run writes goes to the tmpfs mounted at /tmp,
	// including a copy of the image's pre-warmed build cache: grading
	// writes little data but syncs often, and on hosts with slow-flush
	// consumer SSDs those journal commits dominated the run (measured 25s
	// of jbd2 wait vs 0.3s of actual work on the homelab). RAM-only
	// writes make grading time independent of the node's disk. The cp is
	// best-effort: non-Go runners have no /gocache and don't need one.
	cmd := fmt.Sprintf("cp -r /gocache /tmp/gocache 2>/dev/null; tar -chf - -C /job %s %s | /run.sh", codeFile, testFile)
	pod := map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata":   map[string]any{"name": name, "labels": podLabels},
		"spec": map[string]any{
			"restartPolicy":                "Never",
			"activeDeadlineSeconds":        activeDeadlineSeconds,
			"automountServiceAccountToken": false,
			"containers": []map[string]any{{
				"name":            "grade",
				"image":           image,
				"imagePullPolicy": "Never",
				"command":         []string{"/bin/sh", "-c", cmd},
				"env":             []map[string]any{{"name": "GOCACHE", "value": "/tmp/gocache"}},
				"volumeMounts": []map[string]any{
					{"name": "job", "mountPath": "/job", "readOnly": true},
					{"name": "tmp", "mountPath": "/tmp"},
				},
				// Parity with the docker grader's --memory/--cpus caps.
				// The tmpfs pages below count against this limit, hence
				// 512Mi rather than the docker grader's 256m.
				"resources": map[string]any{
					"limits": map[string]any{"memory": "512Mi", "cpu": "1"},
				},
				// Parity with --cap-drop=ALL --security-opt=no-new-privileges.
				"securityContext": map[string]any{
					"allowPrivilegeEscalation": false,
					"capabilities":             map[string]any{"drop": []string{"ALL"}},
				},
			}},
			"volumes": []map[string]any{
				{"name": "job", "configMap": map[string]any{"name": name}},
				{"name": "tmp", "emptyDir": map[string]any{"medium": "Memory", "sizeLimit": "256Mi"}},
			},
		},
	}
	return g.api(ctx, http.MethodPost, "pods", pod, nil)
}

// podLogs fetches the pod's combined output. Log loss is tolerable — a
// grade still lands on the exit code — so errors degrade to empty output
// rather than failing the submission.
func (g *Grader) podLogs(ctx context.Context, name string) string {
	body, err := g.raw(ctx, http.MethodGet, fmt.Sprintf("pods/%s/log?limitBytes=%d", name, maxLogBytes), nil)
	if err != nil {
		g.logger.Warn("fetch grading pod logs", "pod", name, "err", err)
		return ""
	}
	return string(body)
}

// delete removes a grading object. It runs on its own deadline, detached
// from the grade's context: cleanup must happen exactly when the grade is
// over — including when it was canceled, when a timed-out pod would
// otherwise run out its deadline and linger as an object.
func (g *Grader) delete(resource, name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// gracePeriodSeconds=0: grading pods have nothing to shut down
	// cleanly, and a timed-out one should stop burning CPU now.
	if _, err := g.raw(ctx, http.MethodDelete, resource+"/"+name+"?gracePeriodSeconds=0", nil); err != nil {
		g.logger.Warn("cleanup grading object", "resource", resource, "name", name, "err", err)
	}
}

// api performs a namespaced core-v1 API call with a JSON body and decodes
// the response into out (when non-nil).
func (g *Grader) api(ctx context.Context, method, path string, body, out any) error {
	var payload io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = strings.NewReader(string(b))
	}
	resp, err := g.raw(ctx, method, path, payload)
	if err != nil {
		return err
	}
	if out != nil {
		return json.Unmarshal(resp, out)
	}
	return nil
}

func (g *Grader) raw(ctx context.Context, method, path string, body io.Reader) ([]byte, error) {
	url := fmt.Sprintf("%s/api/v1/namespaces/%s/%s", g.cfg.APIServer, g.cfg.Namespace, path)
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	token, err := g.cfg.Token()
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := g.cfg.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxLogBytes+4096))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, firstLine(b))
	}
	return b, nil
}

func firstLine(b []byte) string {
	s := strings.TrimSpace(string(b))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

func podName() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "gc-grade-" + hex.EncodeToString(b)
}
