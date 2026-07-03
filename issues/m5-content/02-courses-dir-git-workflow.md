# courses/ directory + publish workflow

## Context

Chosen middle ground from the "files vs DB" discussion: canonical course
markdown lives in the repo for git history and PR review, while the running
system keeps the ingest API (live publishing, submission FKs, scoring SQL).

## Work

- `courses/` directory holding one .md per course variant (move seed/ file
  there or keep seed/ as fixtures only).
- `make publish` — loops `getcracked seed --url $GC_URL` over courses/*.md
  (needs GC_API_KEY); document agent workflow: agents PR markdown into
  courses/, publish happens on merge.
- Optional later: CI hook doing the publish on merge.

## Done when

Editing a course file + `make publish` updates production and bumps the
variant version.
