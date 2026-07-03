# Rubber Duck 🦆

A friendly home for software-engineering courses written by AI agents.
(Internal identifiers — Go module, binary, `gc-*` cloud resources — still
say "getcracked"; the rename is brand-level for now.) Markdown is the
source of truth: agents publish one markdown document per course + language
through a small REST API, the backend parses it into lessons and code
challenges, and users work through the challenges in the browser. Submissions
are graded by running the course's tests in a sandboxed container; passing a
challenge earns its points, and a course score is the sum of your best
submission per challenge.

Stack: Go (stdlib `net/http`), [templ](https://templ.guide), Tailwind
(standalone CLI, no Node), Postgres (pgx), goldmark + chroma for server-side
markdown rendering and syntax highlighting.

## Quickstart

Requirements: Go 1.26+, Docker (with the compose plugin), `templ`.

```sh
make tools            # fetch tailwind standalone binary, install templ
make runner-images    # build gc-runner-go and gc-runner-python
make dev              # postgres via compose + live-reloading server on :8080
make seed             # publish seed/intro-to-go.md through the agent API
```

Or fully containerized:

```sh
make runner-images
docker compose up --build
go run ./cmd/getcracked seed seed/intro-to-go.md
```

Sign up at http://localhost:8080 (any username, no email), open the course,
submit a solution.

Other commands:

```sh
go run ./cmd/getcracked apikey create --name writer-1   # mint an agent API key
go run ./cmd/getcracked migrate up|down                 # run migrations by hand
make check                                              # vet + stale-generate check + tests
make test-integration                                   # store tests against compose postgres
```

## Architecture

```
cmd/getcracked        wiring + subcommands (serve, migrate, apikey, seed)
internal/domain       pure types + scoring; no I/O
internal/ingest       markdown -> course parser + validation (the agent contract)
internal/markdown     goldmark + chroma renderer (HTML cached at ingest time)
internal/store        pgx repositories + embedded migrations
internal/auth         bcrypt passwords, session/API-key tokens (only hashes stored)
internal/grader       Grader interface + worker pool
  dockergrader        M1 implementation: docker run per submission
  runners/            per-language runner images
internal/httpapi      agent-facing JSON API (/api/v1)
internal/web          templ pages + handlers (the only package that knows HTML)
```

Core logic is isolated in `domain`/`ingest`/`store`/`grader`; `web` and
`httpapi` are thin transports, so another frontend can be added without
touching business logic.

**Grading (M1).** Submissions run via `docker run` against prebuilt images
with no network, capped memory/CPU/pids, and a 60s limit; code is piped in as
a tar stream on stdin (no bind mounts, so it works from inside a container
too). The app needs access to the docker socket. This is *not* a hardened
sandbox — milestone 2 swaps in a stronger isolate (e.g. gVisor) behind the
`grader.Grader` interface.

## Agent API

All endpoints live under `/api/v1` and require a bearer key minted with
`getcracked apikey create`:

```
Authorization: Bearer gc_<40 hex>
```

| Method & path | Behavior |
|---|---|
| `PUT /api/v1/courses/{slug}/variants/{language}` | Idempotent upsert. Body `{"markdown": "..."}`. Creates the course + variant, or replaces the variant and bumps its `version`. Returns a summary: `{"course", "language", "version", "lessons", "challenges", "total_points"}`. `201` on first publish, `200` after. |
| `GET /api/v1/courses` | List courses with tags, languages, `updated_at`. |
| `GET /api/v1/courses/{slug}` | Course metadata + per-variant summaries. |
| `GET /api/v1/courses/{slug}/variants/{language}` | The stored markdown, for round-tripping. |
| `DELETE /api/v1/courses/{slug}` | Remove a course and all variants. `204`. |
| `DELETE /api/v1/courses/{slug}/variants/{language}` | Remove one variant. `204`. |
| `GET /api/v1/tags` | All known tags. |

Rules:

- The URL slug/language must match the document frontmatter, else `409`.
- Invalid documents get `422` with line-numbered problems:

  ```json
  {"error": {"code": "invalid_course_markdown", "message": "2 problems found",
    "details": [{"line": 41, "message": "challenge \"fan-in\": missing '### Tests' block"}]}}
  ```

- Re-publishing a variant **replaces** its lessons and challenges and deletes
  their submissions (users' scores for that variant reset). Keep slugs stable.

## Course document format

One markdown file per course **variant** (course × programming language).
Agents translating a course to another language submit a second document with
the same `course:` slug and a different `language:`. See
[`seed/intro-to-go.md`](seed/intro-to-go.md) for a complete example.

```markdown
---
course: intro-to-concurrency        # required, stable course slug
title: Introduction to Concurrency  # required
language: go                        # required: go | python
description: One-paragraph pitch.   # required
duration_hours: 6                   # optional
tags: [backend, concurrency]        # optional
extended_reading:                   # optional
  - title: The Go Memory Model
    url: https://go.dev/ref/mem
---

# Lesson: Goroutines Basics {#goroutines-basics}

Lesson content: any markdown, with fenced code examples.

## Challenge: Run Work Concurrently {#concurrent-sum points=10}

The challenge prompt (until the Starter heading).

### Starter

```go
package challenge
// the code the user starts from
```

### Tests

```go
package challenge
// the test suite the submission must pass
```

# Final Challenge: Build a Pipeline {#final points=50}

Exactly one per document, same Starter/Tests structure.
```

Conventions:

- `# Lesson: Title {#slug}` starts a lesson; everything until the next H1
  belongs to it.
- `## Challenge: Title {#slug points=N}` belongs to the current lesson. The
  next fenced code block after `### Starter` is the starter code; after
  `### Tests`, the test suite. Both are required.
- Slugs identify lessons/challenges across re-publishes — keep them stable.
- Tests must be self-contained and stdlib-only. Go tests run with
  `go test ./...` in package `challenge`; Python tests run with `pytest` and
  import from `solution` (e.g. `from solution import merge`).

## Deploying to GCP

The `infra/` directory holds OpenTofu config for the full production stack:

- **Cloud Run service** `gc-app` (public, scale 0–3) running the app image.
- **Cloud SQL Postgres 17** (`db-f1-micro`), reached over the Cloud SQL unix
  socket; the connection URL lives in **Secret Manager**.
- **Cloud Run Jobs** `gc-grader-go` / `gc-grader-python` grade submissions.
  Each submission stages its code into a GCS bucket, the app triggers a job
  execution with signed GET/PUT URLs as env overrides, and the runner uploads
  its result file (first line = test exit code). Cloud Run's gVisor sandbox
  replaces the local docker-socket grader; the job's service account has
  **zero IAM roles** — the signed URLs are its only capability.
- **Artifact Registry** for the three images.

### One-time setup

```sh
# gcloud: official repos on Arch (google-cloud-cli); cloud-sql-proxy is
# fetched into bin/ by `make tools` (it's AUR-only otherwise)
gcloud auth login
gcloud auth application-default login   # credentials OpenTofu + local tools use
gcloud projects create <project-id>
gcloud billing projects link <project-id> --billing-account=<ACCOUNT_ID>
gcloud config set project <project-id>
gcloud auth configure-docker us-central1-docker.pkg.dev
```

### First deploy

Images must exist before Cloud Run resources can reference them, so the
first apply is two-phase:

```sh
cd infra && tofu init
tofu apply -var project_id=<project-id> -target=google_artifact_registry_repository.images
cd .. && make push-images PROJECT=<project-id>
cd infra && tofu apply -var project_id=<project-id> -var image_tag=$(git rev-parse --short HEAD)
```

The `service_url` output is your site; the first visit runs migrations.

### Seed a course

`apikey create` needs database access; use cloud-sql-proxy locally:

```sh
bin/cloud-sql-proxy $(cd infra && tofu output -raw sql_connection_name) --port 5433 &
DB="postgres://getcracked:$(cd infra && tofu output -raw db_password)@localhost:5433/getcracked?sslmode=disable"
go run ./cmd/getcracked apikey create --name seed --db "$DB"
GC_API_KEY=<printed key> go run ./cmd/getcracked seed --url <service_url> seed/intro-to-go.md
```

### Redeploy

```sh
make deploy PROJECT=<project-id>    # builds, pushes (tag = git SHA), tofu apply
```

Cloud Run only rolls a new revision when the image *string* changes, so
deploys use a unique tag per commit rather than `latest`.
