# Rubber Duck 🦆

A community-maintained home for software-engineering courses.
(Internal identifiers — Go module, binary, `gc-*` cloud resources — still
say "getcracked"; the rename is brand-level for now.) Every course is one
markdown document per language; anyone with an account can propose a change
(in the browser or with the `duck` CLI), the community reviews it on
`/proposals`, and it publishes once it collects enough approvals — or one
admin approval. The backend parses the markdown into lessons and code
challenges, and users work through the challenges in the browser.
Submissions are graded by running the course's tests in a sandboxed
container; passing a challenge earns its points, and a course score is the
sum of your best submission per challenge.

The database is the source of truth for course content; `courses/*.md` in
this repo is a mirror kept in sync by a scheduled GitHub Action (see
"Course content workflow").

Stack: Go (stdlib `net/http`), [templ](https://templ.guide), Tailwind
(standalone CLI, no Node), Postgres (pgx), goldmark + chroma for server-side
markdown rendering and syntax highlighting.

## Quickstart

Requirements: Go 1.26+, Docker (with the compose plugin), `templ`.

```sh
make tools            # fetch tailwind standalone binary, install templ
make runner-images    # build gc-runner-go, gc-runner-python, gc-runner-c
make dev              # postgres via compose + live-reloading server on :8080
make seed             # import seed/intro-to-go.md + courses/*.md straight into the local db
```

Or fully containerized:

```sh
make runner-images
docker compose up --build
go run ./cmd/duckserver seed seed/intro-to-go.md
```

Sign up at http://localhost:8080 (any username, no email), open the course,
submit a solution.

Other commands:

```sh
go run ./cmd/duckserver user promote --username <u>     # make an account an admin (local db)
go run ./cmd/duckserver migrate up|down                 # run migrations by hand
make check                                              # vet + stale-generate check + tests
make test-integration                                   # store tests against compose postgres
```

## Architecture

```
cmd/duckserver        wiring + subcommands (serve, migrate, user, seed)
internal/domain       pure types + scoring; no I/O
internal/ingest       markdown -> course parser + validation (the course document contract)
internal/markdown     goldmark + chroma renderer (HTML cached at ingest time)
internal/diff         patience line-diff for the proposal review UI
internal/store        pgx repositories + embedded migrations
internal/auth         bcrypt passwords, session/CLI tokens (only hashes stored)
internal/grader       Grader interface + worker pool
  dockergrader        M1 implementation: docker run per submission
  runners/            per-language runner images
internal/httpapi      JSON API (/api/v1): public course reads + proposal endpoints
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

## HTTP API

Everything lives under `/api/v1`. **Reads are public** — course content is
public on the web, and credential-free reads are what let the courses/
mirror sync run from a plain GitHub Action. **Proposal endpoints require a
`gc_u_...` user CLI token** (the same one `duck submit` uses; mint with
`duck auth login` or on `/profile`):

```
Authorization: Bearer gc_u_<40 hex>
```

There is no direct-publish endpoint anymore: the old agent `PUT`/`DELETE`
routes (and the `gc_` agent API keys that authenticated them) were removed
when course changes moved to the proposal workflow. Publishing happens when
a proposal is approved on the site.

| Method & path | Auth | Behavior |
|---|---|---|
| `GET /api/v1/courses` | public | List courses with tags, languages, `updated_at`. |
| `GET /api/v1/courses/{slug}` | public | Course metadata + per-variant summaries. |
| `GET /api/v1/courses/{slug}/variants/{language}` | public | The stored markdown + its `version`: `{"markdown": "...", "version": 3}`. |
| `GET /api/v1/courses/{slug}/variants/{language}/challenges` | public | Starter and test code for each challenge, used by `duck test` local runs. |
| `GET /api/v1/tags` | public | All known tags. |
| `GET /api/v1/export` | public | Every live variant's source: `{"variants": [{"course", "language", "version", "markdown"}]}` — what the courses/ mirror sync reads. |
| `POST /api/v1/proposals` | token | Open a proposal. Body `{"markdown": "...", "title"?, "summary"?, "course"?, "language"?}` — course/language come from the frontmatter; sending them too just cross-checks (`409 slug_mismatch` on disagreement). `201` with `{"id", "base_version", "revision", "status", "url", ...}`. One open proposal per user per variant: `409 duplicate_proposal` otherwise. |
| `PUT /api/v1/proposals/{id}` | token | Replace your open proposal's content. Bumps `revision` (resetting approvals) and re-captures `base_version` from the live variant (how you rebase). `404` if not yours, `409 proposal_closed` if closed. |
| `GET /api/v1/proposals?mine=1` | token | Your proposals, newest first. |
| `GET /api/v1/proposals/{id}` | token | One proposal including its markdown. |
| `POST /api/v1/proposals/{id}/withdraw` | token | Close your own open proposal. `204`. |

Rules:

- Proposal documents are validated on create/update; invalid documents get
  `422` with line-numbered problems:

  ```json
  {"error": {"code": "invalid_course_markdown", "message": "2 problems found",
    "details": [{"line": 41, "message": "challenge \"fan-in\": missing '### Tests' block"}]}}
  ```

- Publishing an approved proposal updates the variant's lessons and
  challenges **in place, keyed by slug** — submissions and scores survive. A
  challenge whose slug leaves the document is archived (hidden from the
  course, its submissions kept as history); if the slug later returns, the
  challenge revives with its history reattached. Slugs are identity: keep
  them stable, and rename only when you intend "this is a different
  challenge".
- A proposal records the live variant `version` it was authored against
  (`base_version`; `0` = the variant doesn't exist yet, i.e. a new course).
  If the live variant moves past that base before the proposal publishes,
  publishing is blocked ("needs rebase") until the proposer updates the
  proposal — the update re-captures the base — so newer content is never
  silently overwritten.

## Course document format

One markdown file per course **variant** (course × programming language).
Agents translating a course to another language submit a second document with
the same `course:` slug and a different `language:`. See
[`seed/intro-to-go.md`](seed/intro-to-go.md) for a complete example.

```markdown
---
course: intro-to-concurrency        # required, stable course slug
title: Introduction to Concurrency  # required
language: go                        # required: go | python | c
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
- C tests are a plain C program: the test file has `main()`, declares
  prototypes for the solution functions it exercises, and is compiled
  together with the solution (`cc solution.c test_solution.c`). It must exit
  non-zero if any test fails, and should print one `--- PASS: name` /
  `--- FAIL: name` line per test case (the `go test -v` format) so the
  grader can score partial credit; run every test rather than aborting on
  the first failure.

## Course content workflow

**The database is the source of truth.** Course changes are made through
the proposal workflow (browser or CLI), reviewed on `/proposals`, and
published on approval. `courses/` in this repo is a **mirror** — one file
per course × language, named `<course-slug>-<language>.md` — kept in sync
by `.github/workflows/course-sync.yml`: every six hours it fetches the
public `GET /api/v1/export`, rewrites `courses/*.md`, and opens an
auto-merging PR when the mirror has drifted. The mirror gives content git
history and lets CI (`internal/ingest`'s `TestCanonicalCoursesParse`) keep
verifying every published document parses. (`seed/intro-to-go.md` is a
separate fixture the ingest/store tests read directly.)

Local dev and break-glass imports write markdown straight into the
database, bypassing review — `duckserver seed` is idempotent (a document
byte-identical to what's stored is skipped, so versions don't bump
spuriously):

```sh
make seed                                    # local: seed fixture + courses/*.md
make import-courses-prod                     # BREAK-GLASS: import courses/*.md into prod
make export-courses DUCK_URL=http://localhost:8080   # regenerate the mirror locally
```

### The proposal workflow

Any logged-in account can propose a change to any course, or a brand-new
course; there's no contributor role to request. Reviews are how quality is
enforced:

- **Open a proposal.** Hit "Edit" on a course variant page (or "+ New
  course" / "+ Add language variant" on the catalog/course pages, which
  seed a valid document template), change the markdown, and submit — or use
  `duck propose` from the CLI. The document is validated on submission;
  nothing invalid ever enters review. You get one open proposal per course
  variant; proposing again updates it.
- **Review.** `/proposals` lists open proposals; each shows a line diff
  against the live document, the review history, and approve/reject forms.
  Anyone logged in can review, except their own proposal.
- **Publish.** A proposal publishes automatically at
  `GC_APPROVAL_THRESHOLD` approvals (default 3; approvals must be on the
  proposal's current revision), or immediately when an **admin** approves.
  An admin rejection closes it. Admins may approve their own proposals —
  the bootstrap case for a small deployment. Publishing attributes the
  variant to the proposer and preserves learner data (see the slug rules
  above).
- **Stay current.** If the live course changes while a proposal is open,
  the proposal is marked "needs rebase" and can't publish until the
  proposer updates it (which also resets earlier approvals — reviewers
  approve content, not intent).

Admins are minted by an operator: `duckserver user promote --username <u>`
(via `make psql-prod`'s cloud-sql-proxy pattern for production).

**The editor.** A raw-markdown textarea (CodeMirror-enhanced) with a live
preview pane rendering via `POST /preview/markdown`, debounced as you type.
Failed submissions re-render with line-numbered problems and keep exactly
what you typed. The preview is read-only and decoupled from submitting.

## Local testing with `duck`

`duck` runs a course's tests locally with your own toolchain — no Docker —
and never makes you wait on the server. `duck submit` runs the tests, sends
the solution together with the local verdict, and the score lands instantly;
the server re-grades in the background as an **audit** (informational only —
it badges the submission "verified" on agreement or shows both outputs on a
mismatch, but never rewrites the score). Server-side grading — a Cloud Run
Job execution taking minutes in production — is still what browser
submissions and `duck submit --remote` wait on, and what every audit runs
through.

Prebuilt binaries (linux/darwin/windows, amd64/arm64) are published to
[GitHub Releases](https://github.com/michael-duren/rubber-duck/releases/latest)
by CD's `release-cli` job on every deploy; `duck version` reports the release
tag (or the module version for `go install` builds):

```sh
go install github.com/michael-duren/rubber-duck/cmd/duck@latest
# or, from a checkout:
go install ./cmd/duck   # or: go build -o duck ./cmd/duck

duck pull intro-to-concurrency/go   # scaffolds ./intro-to-concurrency-go/<slug>/
duck test concurrent-sum            # go test ./... / pytest / cc, no submission
duck submit concurrent-sum          # runs tests + submits; score is immediate
duck submit concurrent-sum --remote # skip the local run; wait for server grading
```

If the course language's toolchain isn't installed, `duck submit` falls back
to `--remote` behavior automatically.

`duck submit` needs a user token: run `duck auth login`, or mint one from
your profile page ("Create CLI token") and either set `DUCK_TOKEN` or save
it to `~/.config/duck/token`. The `/tokens` page on any deployment
documents tokens end to end. `duck pull` defaults to
`http://localhost:8080`; override with `--base` or `DUCK_BASE_URL` (the
base URL is then remembered in the scaffolded course dir's
`.duck-course.json` for `test`/`submit`).

## Authoring courses with `duck propose`

The author-facing flow mirrors the learner flow above but works on the
whole course document: fetch it, edit with your own editor, validate
locally, then submit it as a proposal for review. `duck propose` needs the
same user token as `duck submit` (`duck auth login`); pulling and linting
need no credentials at all.

```sh
duck educator pull intro-to-concurrency/go   # fetch the markdown + a .meta.json sidecar
$EDITOR intro-to-concurrency-go.md
duck ed lint                                 # validate offline; same checks as the server
duck propose --summary "clarify the goroutines lesson"
```

```
proposed intro-to-concurrency/go as proposal #12
review it at https://duckgc.com/proposals/12
```

Details worth knowing:

- `educator pull` writes the markdown plus a `<file>.md.meta.json` sidecar
  recording the server, course, language, and version pulled. It refuses to
  overwrite a locally-edited file without `--force`.
- `duck propose` derives course/language from the document's
  **frontmatter**, so it also works with no sidecar at all — write a brand
  new course document from scratch and propose it.
- Proposing again from the same file updates your open proposal (the
  sidecar remembers its id; without a sidecar the server's
  `duplicate_proposal` answer routes the update automatically). Updating
  resets earlier approvals — reviewers approve content.
- `duck proposals` lists your proposals with approval counts;
  `duck proposals status <id>` shows one. If the course changed underneath
  your proposal it's flagged `NEEDS REBASE`: re-pull, reapply your edits,
  and `duck propose` again.
- `lint` exits non-zero on problems, so it composes with a pre-commit hook
  or CI step. A locally-invalid document never generates a request.

The old `duck educator push`, which published directly with no review, is
retired and says so if invoked.

## Deploying to GCP

The `infra/` directory holds OpenTofu config for the full production stack:

- **Cloud Run service** `gc-app` (public, scale 0–3) running the app image.
- **Cloud SQL Postgres 17** (`db-f1-micro`), reached over the Cloud SQL unix
  socket; the connection URL lives in **Secret Manager**.
- **Cloud Run Jobs** `gc-grader-go` / `gc-grader-python` / `gc-grader-c`
  grade submissions.
  Each submission stages its code into a GCS bucket, the app triggers a job
  execution with signed GET/PUT URLs as env overrides, and the runner uploads
  its result file (first line = test exit code). Cloud Run's gVisor sandbox
  replaces the local docker-socket grader; the job's service account has
  **zero IAM roles** — the signed URLs are its only capability.
- **Artifact Registry** for the app and runner images.

### Architecture

```
                         ┌─────────────────────┐
   users ───────────────▶│  Cloud Run Service   │  gc-app (public, scale 0-3)
                         │      "gc-app"        │
                         └──┬────┬────┬────┬────┘
                            │    │    │    │
              Unix socket   │    │    │    │ triggers job execution
              (no network)  │    │    │    │ (signed URLs as env overrides)
                            ▼    │    │    ▼
                  ┌──────────┐   │    │   ┌────────────────────────┐
                  │Cloud SQL │   │    │   │   Cloud Run Jobs        │
                  │ Postgres │   │    │   │ gc-grader-go/python/c   │
                  └──────────┘   │    │   └───────────┬─────────────┘
                                 │    │               │
                     read secret │    │ stage/fetch    │ fetch code via
                     at startup  │    │ via signed URL │ signed GET,
                                  ▼    ▼                │ push result via
                  ┌──────────────┐  ┌──────────────┐    │ signed PUT
                  │Secret Manager│  │  GCS bucket   │◀───┘
                  │ DATABASE_URL │  │ (grading      │
                  └──────────────┘  │  staging)     │
                                     └──────────────┘
```

Artifact Registry (`getcracked` repo) holds the app image (`getcracked`)
and one runner image per language (`gc-runner-go`, `gc-runner-python`,
`gc-runner-c`); `gc-app` and the grader jobs pull from it, and CD pushes
new tags there on every merge to `main`.

**Trust boundary — two service accounts:**

- `gc-app`'s SA holds real permissions: `cloudsql.client`, `run.viewer`
  (project-scoped — polling a job execution's LRO needs `run.operations.get`,
  which job-scoped roles can't see), `run.developer` scoped to each grader
  job (`runWithOverrides` needs more than `run.invoker`),
  `secretmanager.secretAccessor` on the DB URL secret, and
  `iam.serviceAccountTokenCreator` on itself (keyless V4 URL signing).
- `gc-grader`'s SA has **zero** IAM roles. It can only reach the two GCS
  objects it's handed via signed URLs — a capability baked into the URL, not
  an IAM grant — so even fully-compromised submission code running inside
  the job can't do anything with that identity.

**Grading flow:** the pool worker (`internal/grader/pool.go`) stages a
submission's code + tests into the GCS bucket, generates a signed GET (for
the job to read) and signed PUT (for its result), and starts a
`gc-grader-{lang}` job execution with those URLs as env overrides. The job
fetches, runs the tests, and uploads a result file (first line = exit code).
`gc-app` polls the execution via `run.viewer` until it completes, reads the
result back from GCS, and updates the submission. Browser submissions wait
on this flow; CLI-claimed submissions run it as a background audit that
fills the `audit_*` columns without touching the claimed verdict.

`infra/network.tf` adds a VPC + Serverless VPC Access connector + private
DNS override + firewall so grader-job egress is locked to GCS only — see
[`docs/infra.md`](docs/infra.md) for CI/CD setup and why that file isn't
applied yet.

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

### Seed courses

A fresh deploy has an empty catalog. `make import-courses-prod` wires up
cloud-sql-proxy and the tofu outputs and imports every course in `courses/`
straight into the prod database — the documented break-glass path, which is
exactly what bootstrapping is:

```sh
make import-courses-prod
```

(The manual equivalent: run `bin/cloud-sql-proxy <connection-name> --port
5433`, then `go run ./cmd/duckserver seed --db
"postgres://getcracked:<password>@localhost:5433/getcracked?sslmode=disable"
courses/<file>.md` per course, with both values from `tofu -chdir=infra
output`.)

After bootstrap, content changes go through the in-app proposal workflow —
mint your first admin with `go run ./cmd/duckserver user promote --username
<you> --db <proxy URL>` so approvals can publish instantly while the
community is small.

### Redeploy

```sh
make deploy PROJECT=<project-id>    # builds, pushes (tag = git SHA), tofu apply
```

Cloud Run only rolls a new revision when the image *string* changes, so
deploys use a unique tag per commit rather than `latest`.
