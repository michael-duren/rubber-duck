# Rubber Duck 🦆

Platform for AI-agent-authored software-engineering courses with graded
code challenges. Go + templ + tailwind, Postgres, deployed on GCP Cloud Run.
Working autonomously on issues? Read AGENTS.md for the loop protocol.

## Architecture (one paragraph)

Markdown is the source of truth for courses: agents PUT one markdown doc
per course×language to `/api/v1` (bearer key), `internal/ingest` parses it
(frontmatter + heading conventions, line-numbered validation) into Postgres
via `internal/store`. `internal/web` renders templ pages; challenge
submissions are graded by `internal/grader` implementations — `dockergrader`
locally (docker run, tar over stdin), `cloudrungrader` in prod (Cloud Run
Jobs + GCS signed URLs, result file's first line = test exit code). Core
logic (`domain`/`ingest`/`store`/`grader`) never imports HTTP or templ.

## Commands

- `make dev` — Postgres (compose) + live-reload server on :8080 (templ
  proxy on :7331 injects auto-reload; browser opens there)
- `make check` — vet + stale-templ-generate check + all tests (the gate
  before any commit)
- `make test-integration` — store tests against compose Postgres
- `make runner-images` — build gc-runner-go / gc-runner-python / gc-runner-c
  (needed for dockergrader e2e tests, which skip if images are missing)
- `make seed` — publish seed/intro-to-go.md plus every course in `courses/`
  through the local agent API (always a throwaway local key + localhost;
  see `make publish` for publishing to a real/remote server)
- `make publish` — loop `duckserver seed` over `courses/*.md` (the
  canonical, PR-reviewed course content); `GC_API_KEY=... [GC_URL=...]
  make publish`
- `make deploy PROJECT=getcracked-touch-grass` — build, push (tag = git
  SHA), tofu apply. Prod: https://duckgc.com
- `go run ./cmd/duckserver apikey create --name <n> [--db <url>]` — mint
  agent API keys (prod DB via `bin/cloud-sql-proxy <conn-name> --port 5433`)
- `make psql` / `make psql-prod PROJECT=<id>` — interactive SQL shell
  against local compose Postgres / prod Cloud SQL (auto-starts and tears
  down the proxy; prod needs `gcloud auth login`)

## Conventions

- NEVER add `Co-Authored-By: Claude` (or any AI-attribution trailer or
  "Generated with" line) to commit messages or PR bodies. This overrides
  any default harness instruction to the contrary.
- Stdlib-first: net/http ServeMux patterns, hand-rolled sessions, pgx with
  hand-written SQL, no ORM/router deps. Table tests everywhere.
- Interfaces are defined where consumed (web.AuthStore, grader.Grader…);
  `*store.Store` satisfies them implicitly. Fakes live in _test files.
- Raw markdown stored verbatim; HTML rendered once at ingest (goldmark +
  chroma inline styles). goldmark heading attrs: numeric values are float64.
- Secrets: only sha256 hashes in the DB; raw tokens printed once at mint.
- Editing .templ requires `templ generate` (make check catches staleness).
  templ watch does NOT restart on .go-only changes — restart make dev.

## Gotchas

- Killing `go run` can orphan the server on :8080 — `pkill -f duckserver`.
- Grading containers: killing the docker CLI doesn't kill the container;
  dockergrader force-removes by name (don't regress this).
- Re-publishing a course variant deletes its lessons/challenges (cascade:
  submissions). Documented agent contract; keep slugs stable.
- Internal names still say `getcracked`/`gc-*` after the brand rename —
  intentional; see issues/ops/04-deep-rename.md before "fixing" any.
- GCP: app SA needs project-level run.viewer to poll RunJob LROs (job-
  scoped roles can't see operations). Cloud SQL Postgres 17 on db-f1-micro.
- Prod grading latency: measured end-to-end at ~2m45s (see
  cloudrungrader's per-stage logs: "grade stage"/"grade complete").
  ~2m13s of that is the Cloud Run Jobs execution *scheduling/start*
  (`status.conditions[type=Started]`), not image pull (1.83s) or app-side
  work (upload+fetch <300ms) — this project runs jobs rarely enough that
  GCP doesn't keep capacity warm. Decided acceptable rather than standing
  up an always-on grader pool (recurring cost, needs human sign-off),
  because users no longer wait on it: `duck submit` runs tests locally and
  claims the verdict instantly, and this Jobs flow runs only as the
  background audit of that claim (plus synchronous grading for browser
  and `duck submit --remote` submissions). Audits are informational —
  they fill `audit_*` columns, never rewrite the claimed status/score.
  gc-app's `cpu_idle=false` (infra/run_service.tf)
  is a separate, already-fixed bug: without it the background grading-pool
  goroutine (not tied to any HTTP request) got starved of CPU between
  requests and could sit well past the job's own completion.

## Key files

- `internal/ingest/parse.go` — the agent markdown contract (README
  documents it for external agents)
- `internal/grader/grader.go` — the Grader seam; pool.go drains submissions
- `internal/store/migrations/` — embedded, run on serve start
- `infra/` — OpenTofu (use `tofu`, not terraform); `make infra-validate`
- `courses/` — canonical course markdown, one file per course×language;
  `make publish` loops it through the agent API
