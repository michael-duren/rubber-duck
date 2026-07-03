# Run goimports before go test in the go runner

## Context

Submissions are compiled as a single file, so a solution using `sync`
without `import "sync"` fails with `undefined: sync` — a confusing failure
mode for something the toolchain can fix automatically.

## Work

- Add `goimports` to `internal/grader/runners/go/Dockerfile` (go install at
  build time, pre-warmed).
- In `run.sh`, run `goimports -w solution.go` (solution only — not the test
  file, which agents author deliberately) before `go test`.

## Done when

A submission that omits a stdlib import passes instead of failing to build,
in both stdin (local docker) and URL (Cloud Run) modes.
