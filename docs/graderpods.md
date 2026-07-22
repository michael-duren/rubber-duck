# Grader as its own service (build plan)

Goal: move grading out of duckserver into its own pod(s) behind a k8s
Service. This is a you-type-it project (praxis: microservices); this doc is
the map, not the code.

## The design fork — decide before typing

"Grader in its own pod behind a Service" can mean two things, and the
difference is **where isolation lives**:

- **Option A — grader executes jobs inside its own pod.** Long-running
  `grader-go` Deployment; duckserver POSTs `{code, test_code}`; it runs
  `go test` in-process in a tmpfs workdir and returns the result. Fast
  (~0.3s, no pod spawn), textbook k8s. BUT untrusted code now runs inside a
  long-lived pod: submission N can poison the filesystem/memory for
  submission N+1, and anything it escalates to it keeps until the pod
  restarts. You lose the fresh-sandbox-per-submission property.

- **Option B (recommended) — grader is a control-plane service** that still
  spawns one ephemeral pod per job. The existing `k8sgrader` logic moves
  behind an HTTP service (`graderd`); duckserver just does HTTP. Isolation
  stays per-submission. Gains: duckserver loses its pod-create RBAC (only
  graderd keeps it), independent deploy/scale, grader swappable behind a
  network contract instead of a Go interface. Option A can come later as a
  "warm pool" optimization once the blast-radius tradeoff is understood.

## What to write (Option B), in build order

1. **Contract first.** `POST /grade` with
   `{"language":"go","code":"...","test_code":"..."}` →
   `{"status":"passed|failed|error","output":"...","tests_passed":4,"tests_total":4}`
   (the wire form of `grader.Result`). Synchronous is fine: grades are ~3s
   and the pool already caps concurrency at 2.

2. **`internal/grader/httpgrader/httpgrader.go`** — client implementing
   `grader.Grader` by POSTing to graderd (base URL + http.Client fields).
   Client timeout a bit above the grade budget. Non-200s and transport
   errors are the *infra error* return, never Status "failed". Table tests
   with `httptest.Server` faking graderd: passed/failed/error/timeout/non-200
   (mirror k8sgrader_test.go's style).

3. **`cmd/graderd/main.go`** — small stdlib net/http server, duckserver
   style: `POST /grade` decodes the job, calls the existing
   `k8sgrader.Grader` (reused as a library — it does not move), writes JSON.
   `GET /healthz` → 200 (becomes the readiness probe). Listen addr from
   `PORT` like duckserver.

4. **Wire-up** in `cmd/duckserver/main.go`: new `GC_GRADER=http` case
   reading `GC_GRADER_URL`. graderd itself keeps using
   `k8sgrader.InCluster()`.

5. **Manifests** (crib shapes from deploy/homelab/):
   - `graderd-deployment.yaml`: simplest is to reuse the duckserver image
     with `command: ["graderd"]` (add the second binary to the Dockerfile
     build). `serviceAccountName: duckserver` MOVES here from the duckserver
     deployment. Readiness probe on `/healthz`.
   - `graderd-service.yaml`: ClusterIP, port 80 → targetPort 8080, selector
     matching graderd's pod labels.
   - `deployment.yaml`: duckserver loses `serviceAccountName`, gains
     `GC_GRADER=http` + `GC_GRADER_URL=http://graderd` (kube DNS, same
     namespace).
   - `rbac.yaml`: unchanged subject, but verify only graderd mounts the SA.

6. Deploy script: no changes needed if graderd rides the duckserver image.

## Quiz — answer these before building (rough answers fine)

1. Duckserver dials `http://graderd`. What resolves that name, and what
   sits between the name and an actual graderd pod IP after DNS?
2. Why does the readiness probe matter here in a way it didn't when grading
   was in-process? What goes wrong during a graderd rollout without it?
3. Scale graderd to 3 replicas: do in-flight grades break? What state does
   graderd actually hold?
4. The synchronous POST blocks ~3s per grade. What's the signal that would
   force a move to a queue-based design?

Build order recap: contract → httpgrader (+tests) → graderd → manifests →
flip the env var. Ask for a review after each piece.
