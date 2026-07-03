# gc CLI: pull / test / submit

## Context

Server-side grading takes 30-60s (Cloud Run Job cold start). The natural
workflow is: iterate locally with instant feedback, submit for points when
green. Server grading stays the source of truth for scores.

## Work

New `cmd/gc` (small static binary; `go install`-able):

- `gc pull <course>/<language>` — fetch challenges from the public endpoint
  (issue 02), scaffold `./<course>-<language>/<challenge-slug>/` dirs each
  containing solution + test files (skip dirs that already exist).
- `gc test [slug]` — run the language's native test command in each
  challenge dir (`go test ./...` / `pytest`); requires the user's own
  toolchain, no Docker.
- `gc submit <slug>` — POST the solution file using a user token (issue 03,
  `GC_TOKEN` env or config file at ~/.config/getcracked/token), print the
  submission URL and poll until graded.
- README section documenting the flow.

## Done when

Full loop against production: pull the seed course, fail a test locally,
fix it, `gc submit` shows passed with points.
