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
make runner-images    # build gc-runner-go, gc-runner-python, gc-runner-c
make dev              # postgres via compose + live-reloading server on :8080
make seed             # publish seed/intro-to-go.md + courses/*.md through the agent API
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
make apikey KEY_NAME=writer-1                           # mint an agent API key (local db)
make apikey-prod KEY_NAME=writer-1                      # same, against prod via cloud-sql-proxy
go run ./cmd/duckserver migrate up|down                 # run migrations by hand
make check                                              # vet + stale-generate check + tests
make test-integration                                   # store tests against compose postgres
```

## Architecture

```
cmd/duckserver        wiring + subcommands (serve, migrate, apikey, seed)
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

All endpoints live under `/api/v1` and — except the public challenges
listing, see the table — require a bearer key minted with
`duckserver apikey create`, **or** a human's `gc_u_...` user CLI token (same
one `duck submit`/`duck test` use):

```
Authorization: Bearer gc_<40 hex>
Authorization: Bearer gc_u_<40 hex>
```

The variant `GET` response always includes the variant's `version`,
whichever credential kind authorized it. A user token only changes behavior
on the variant `PUT` (attribution + optional `expected_version`, see below);
every other endpoint behaves the same regardless of which credential kind
authorized it.

| Method & path | Behavior |
|---|---|
| `PUT /api/v1/courses/{slug}/variants/{language}` | Idempotent upsert. Body `{"markdown": "..."}`. Creates the course + variant, or replaces the variant and bumps its `version`. Returns a summary: `{"course", "language", "version", "lessons", "challenges", "total_points"}`. `201` on first publish, `200` after. |
| `GET /api/v1/courses` | List courses with tags, languages, `updated_at`. |
| `GET /api/v1/courses/{slug}` | Course metadata + per-variant summaries. |
| `GET /api/v1/courses/{slug}/variants/{language}` | The stored markdown, for round-tripping. Response also includes the variant's `version`: `{"markdown": "...", "version": 3}`. |
| `DELETE /api/v1/courses/{slug}` | Remove a course and all variants. `204`. |
| `DELETE /api/v1/courses/{slug}/variants/{language}` | Remove one variant. `204`. |
| `GET /api/v1/tags` | All known tags. |
| `GET /api/v1/courses/{slug}/variants/{language}/challenges` | **Public, no auth**: starter and test code for each challenge, used by `duck test` local runs. |

Rules:

- The URL slug/language must match the document frontmatter, else `409`.
- Invalid documents get `422` with line-numbered problems:

  ```json
  {"error": {"code": "invalid_course_markdown", "message": "2 problems found",
    "details": [{"line": 41, "message": "challenge \"fan-in\": missing '### Tests' block"}]}}
  ```

- Re-publishing a variant updates its lessons and challenges **in place,
  keyed by slug** — submissions and scores survive. A challenge whose slug
  leaves the document is archived (hidden from the course, its submissions
  kept as history); if the slug later returns, the challenge revives with
  its history reattached. Slugs are identity: keep them stable, and rename
  only when you intend "this is a different challenge".
- `PUT` also accepts a `gc_u_...` human user token (the same CLI token minted
  for `duck submit`/`duck test`) in place of an agent key. Human-authored
  writes may include an optional `expected_version` field (from a prior
  `GET`'s `version`) for optimistic concurrency: a mismatch is rejected with
  `409 {"error": {"code": "version_conflict", ...}}` instead of overwriting.
  Agent-key behavior is unchanged — `expected_version` is ignored if sent.

## Course document format

One markdown file per course **variant** (course × programming language).
Agents translating a course to another language submit a second document with
the same `course:` slug and a different `language:`. See
[`seed/intro-to-go.md`](seed/intro-to-go.md) for a complete example.

````markdown
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
````

Conventions:

- `# Lesson: Title {#slug}` starts a lesson; everything until the next H1
  belongs to it.
- `## Challenge: Title {#slug points=N}` belongs to the current lesson. The
  next fenced code block after `### Starter` is the starter code; after
  `### Tests`, the test suite. Both are required.
- Slugs identify lessons/challenges across re-publishes — keep them stable.
- A challenge slug must not start with `final-` or with two-plus digits
  (plus at most one letter) and a dash, e.g. `64-bit-ints` — ingest rejects
  these. `duck pull` names challenge directories by prefixing the slug that
  way (`03a-merge`, `final-task-scheduler`), and the CLI strips exactly
  those shapes to map a directory back to its slug — a slug shaped like a
  prefix would strip wrongly. Single leading digits (`3-way-partition`) are
  fine, and the *final* challenge may be named `final-…` (its directory
  gets a second `final-` prepended, which still strips back correctly).
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

`courses/` holds the canonical markdown for every published course variant
— one file per course × language, named `<course-slug>-<language>.md` — so
course content gets git history and PR review like any other change.
(`seed/intro-to-go.md` is a separate, older fixture the ingest/store tests
read directly; it's not part of the publish workflow and may drift from
`courses/intro-to-concurrency-go.md`.)

Agents (or humans) branch and PR markdown changes into `courses/`; publish
after merge:

```sh
GC_API_KEY=<agent key> make publish                       # local server
GC_API_KEY=<agent key> GC_URL=<service_url> make publish  # prod
```

`make publish` loops `duckserver seed --url $GC_URL <file>` over every
`courses/*.md`, bumping each variant's version. Re-publishing updates a
variant's lessons/challenges in place by slug — learner submissions and
scores survive; removed slugs are archived, not deleted — keep slugs
stable (see "Course document format" above).

## Editing courses in the browser

Alongside the agent API and the git-based `courses/*.md` workflow above,
any logged-in user can create or edit course content directly in the
browser — a "click edit" wiki path, not just a contributor/PR path. This
was a deliberate product decision (any authenticated account can edit;
abuse is handled by disabling the account, not by a special role/permission
gate) and is a first step towards the database, rather than `courses/*.md`,
becoming the actual source of truth for course content — for now all three
paths (agent API, git + `make publish`, and the browser editor) write
through the same validation and storage, so content stays consistent no
matter which one was used.

**Where to find it.** A course variant's page (`/courses/{slug}/{lang}`)
has an "Edit" link; a course's page has a "+ Add language variant" link;
the catalog has a "+ New course" link. All three require being logged in
(any account, no special role) and redirect anonymous visitors to
`/login`.

**Creating a course or variant.** "+ New course" / "+ Add language
variant" opens a small form (slug, title, language, description) that
seeds a minimal, already-valid frontmatter template — one lesson and one
final challenge with placeholder starter/test code — into the same editor
described below, rather than creating anything itself. Nothing is
persisted until that first Save.

**Editing and saving.** The editor is a raw-markdown textarea, pre-filled
with the variant's stored source. Saving runs the exact same
`ingest.Parse` / `ingest.ToDomain` / `store.UpsertVariant` path the agent
API's `PUT` uses:

- Invalid markdown re-renders the editor with the same line-numbered
  problems the agent API returns as `422` details — the textarea keeps
  exactly what you typed, never reverting to the last-saved version.
- The page carries the version it was loaded at in a hidden field
  (optimistic concurrency). If someone else saved in the meantime, your
  save is rejected with "someone else changed this since you opened it —
  reload to see their version" instead of silently overwriting their
  change.
- Saving is never destructive to learner data: like `make publish`, it
  updates lessons/challenges in place by slug, so submissions and scores
  survive every save. (There used to be a "saving will reset progress"
  confirmation step here; it disappeared along with the destructive
  behavior it warned about.)

**Live preview.** A preview pane next to the textarea shows the rendered
markdown (headings, code blocks with syntax highlighting) via an AJAX call
to `POST /courses/{slug}/{lang}/edit/preview`, debounced as you type. It's
read-only and decoupled from saving: a slow or failed preview never blocks
or is required for an actual Save, and it never writes anything itself.

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
by the `cli-release` workflow when a semver tag (`v*`) is pushed by hand —
deliberately decoupled from CD's per-merge deploys; `duck version` reports the
release tag (or the module version for `go install` builds):

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

`duck submit` needs a user token, not an agent API key: run `duck auth login`, or
mint one from your profile page ("Create CLI token") and either set
`DUCK_TOKEN` or save it to `~/.config/duck/token`. The `/tokens` page on any
deployment documents both credential kinds (user CLI tokens vs agent API
keys) end to end. `duck pull` defaults to `https://duckgc.com`;
override with `--base` or `DUCK_BASE_URL` (the base URL is then remembered in
the scaffolded course dir's `.duck-course.json` for `test`/`submit`).

## Authoring courses with `duck educator`

`duck educator` (alias `duck ed`) is the author-facing counterpart to the
learner flow above: instead of scaffolding one directory per challenge, it
round-trips a single course-variant markdown document with the same
`/api/v1` endpoints the "Agent API" section documents — `pull` to fetch it,
your own editor to change it, `lint` to validate locally, `push` to publish.
It needs the same user token as `duck submit` — same login as above, see
"Local testing with `duck`" — there's no separate author credential. A
revoked or invalid token fails with `unauthorized: token missing or
revoked`; with no token configured at all, `duck` fails before any network
call and tells you how to mint one. (An agent API key in `DUCK_TOKEN` also
authenticates, but the server then ignores `expected_version` — use your
personal `gc_u_...` token so stale pushes are caught.)

```sh
duck educator pull intro-to-concurrency/go
```

```
pulled intro-to-concurrency-go.md (version 3)
```

This writes two files in the current directory:

- `intro-to-concurrency-go.md` — the variant's raw markdown, exactly as
  stored (the same document a `GET` against the Agent API would return).
- `intro-to-concurrency-go.md.meta.json` — a sidecar recording where the
  file came from and the version it was pulled at:

  ```json
  {
    "base_url": "http://localhost:8080",
    "course": "intro-to-concurrency",
    "language": "go",
    "version": 3
  }
  ```

  `push` reads this sidecar to know which server/course/language to send the
  file back to, and sends its `version` as `expected_version` so a stale
  push — the variant changed since this pull — is rejected instead of
  silently overwritten (see below).

Like the learner flow's `duck pull`, `educator pull` defaults to
`https://duckgc.com`; override with `--base` or `DUCK_BASE_URL` — the
resolved base is recorded in the sidecar so `push` targets the same server.
And it protects local work the same way `push` protects the server's: if the
markdown file already exists with different content (say, unpushed edits),
`pull` refuses to overwrite it unless you pass `--force`.

Now edit `intro-to-concurrency-go.md` directly with your own editor. It's a
plain markdown file in the format described in "Course document format"
above; nothing about the contents is duck-specific.

Before pushing, validate locally — no network round trip:

```sh
duck educator lint intro-to-concurrency-go.md
```

```
intro-to-concurrency-go.md: no problems found
```

A broken document reports the same line-numbered problems `internal/ingest`
would raise on the server side (the same validation runs locally):

```
intro-to-concurrency-go.md: 1 problem(s) found
  line 41: challenge "fan-in": missing '### Tests' block
```

`lint` exits non-zero when problems are found, so it composes with a
pre-commit hook or CI step. `lint`/`push` both take an optional file
argument; run with none and they look for the single `*.meta.json` sidecar
left by `pull` in the current directory, erroring if there's zero or more
than one (pass a path explicitly to disambiguate).

`push` runs this same local lint before touching the network — a
locally-invalid document never generates a request — then `PUT`s the file
back with the sidecar's version as `expected_version`:

```sh
duck educator push intro-to-concurrency-go.md
```

```
pushed intro-to-concurrency-go.md — version 4 (1 lessons, 2 challenges, 30 pts)
```

The sidecar's `version` is updated to the response's new version, so the
next `push` from the same file doesn't need another `pull` first. If
someone else — another author's `pull`/`push`, or a save through the web
editor — changed the variant since this file was pulled, the push is
rejected with `409 version_conflict` and `duck` prints:

```
duck: someone else changed this course variant since you last pulled it — save your edits elsewhere, run `duck educator pull --force` to fetch the latest version, then reapply them and push again
```

The fix is to re-pull (fetching the latest markdown and version) and
reapply your edits — `--force` is needed there precisely because a plain
`pull` refuses to overwrite the locally edited file. `push` itself has no
force/overwrite flag, by design: the server-side conflict can't be
bypassed. A `422` from the server (rare, since `push` already ran the same
validation locally) prints line-numbered problems the same way `lint` does.

This is the same `UpsertVariant` / optimistic-concurrency path as the Agent
API's `PUT` (see "Agent API" above) and the browser editor (see "Editing
courses in the browser" above) — a human using `duck educator`, the web
editor, and an agent's `PUT` against the same variant won't silently clobber
each other; whichever writes last against a stale `expected_version` (or
loaded version, in the editor's case) gets a conflict instead of overwriting.

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

### Seed a course

`apikey create` needs database access; `make apikey-prod` wires up
cloud-sql-proxy and the tofu outputs for you:

```sh
make apikey-prod KEY_NAME=seed
GC_API_KEY=<printed key> go run ./cmd/duckserver seed --url <service_url> seed/intro-to-go.md
```

(The manual equivalent: run `bin/cloud-sql-proxy <connection-name> --port
5433`, then `apikey create --db "postgres://getcracked:<password>@localhost:5433/getcracked?sslmode=disable"`,
with both values from `tofu -chdir=infra output`.)

### Redeploy

```sh
make deploy PROJECT=<project-id>    # builds, pushes (tag = git SHA), tofu apply
```

Cloud Run only rolls a new revision when the image *string* changes, so
deploys use a unique tag per commit rather than `latest`.
