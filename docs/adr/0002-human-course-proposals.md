# ADR 0002: Human course proposals replace the agent publish API

- **Status:** Accepted (merged via PR #87)
- **Date:** 2026-07-17
- **Deciders:** Michael Duren

## Context

Course content used to reach the site through an agent-shaped write API:
`PUT /api/v1/courses/{slug}/variants/{language}` and `DELETE` routes
authenticated by `gc_` API keys, with `courses/*.md` in this repo as the
canonical source and CD re-publishing the whole directory to prod on every
merge to main. Content review happened (if at all) as a GitHub PR against
the markdown mirror — invisible to users of the site, gated on having a
GitHub account, and disconnected from the app's own identity system.

The project's direction (see the "Wikipedia for courses" framing) is
community-maintained content: any logged-in user should be able to propose
a change, other users should review it where they already are (the site),
and publishing should be an outcome of that review — not of holding an API
credential. That also means the **database must become the source of
truth**, with the repo's `courses/` directory demoted to a synced mirror.

## Decision

Replace the agent publish API with an in-app **propose → review → approve →
publish** workflow, flip the sync direction of `courses/`, and remove the
agent credential surface entirely.

### The workflow

- **Anyone logged in can propose**: edit a course in the browser (the old
  editor now opens a proposal instead of writing the variant), draft a
  brand-new course, or `duck propose` from the CLI. Documents are validated
  on submission — line-numbered parse errors *and* a full render check, so
  a d2 diagram that doesn't compile is rejected at authoring time, not
  after the proposal has collected approvals.
- **One open proposal per user per variant** (partial unique index).
  Proposing again collapses into updating the existing proposal.
- **Review on `/proposals`**: open proposals show a unified line diff
  against the live document, review history, and approve/reject forms.
  Self-review is blocked in the store (role read inside the transaction),
  with one carve-out: an admin may approve their own proposal — the
  bootstrap case for a small site with one admin and no quorum.
- **Publish** happens automatically at `GC_APPROVAL_THRESHOLD`
  current-revision approvals from non-proposers (default 3), or instantly
  on one admin approval. Only an *approve* verdict can trigger the publish
  attempt. Admin rejection closes the proposal.
- **Revisions**: a proposer updating content bumps `revision`, which
  invalidates standing approvals (each review records the revision it
  saw). A verdict submitted against a superseded revision is refused, so
  an approval can never count toward content the reviewer never read.
- **Stale protection ("needs rebase")**: a proposal records the live
  variant version it was authored against (`base_version`, 0 for a
  new-course proposal). If the live variant moves past it, publishing
  fails with a version conflict and the proposal stays open; updating the
  proposal re-captures the base (that *is* the rebase) and resets
  approvals. Publishing also passes the revision the approvals were
  counted against — a proposer revision racing the publish is refused
  (`ErrStaleRevision`) instead of shipping the older content.

### Data model (migrations 0009–0012)

- `users.role` (`user`/`admin`), minted only via
  `duckserver user promote` — deliberately no web flow, so there is no
  privilege-escalation surface to defend in the app.
- `proposals` (document, base_version, revision, one-way status:
  `open → published | rejected | withdrawn`) and `proposal_reviews`
  (one row per reviewer per proposal, upserted; `created_at` keeps the
  first review's time, `updated_at` moves on re-review).
- `api_keys` dropped — the table, the store code, the mint command, and
  every route that consumed them.

### Store semantics

`store.PublishProposal` reuses the exact same `upsertVariantTx` every
publish has always used, attributed to the proposer, with `base_version`
as the optimistic-concurrency check. All the learner-data invariants are
unchanged: lessons/challenges diff **by slug**, rows update in place so
submissions and scores survive, removed slugs archive (never delete), and
returning slugs revive with history reattached.

Review/publish races serialize on `SELECT … FOR UPDATE` of the proposal
row; a double-publish loser sees `ErrProposalClosed` and treats it as
already-done. The store's `AddReview` deliberately decides **nothing**
about publishing — the approval threshold is web-layer config, and the
handler owns the decision.

### HTTP API

- **All `/api/v1` reads are public** — course content is public on the
  web, and credential-free reads are what let the mirror sync run from a
  plain GitHub Action. New `GET /api/v1/export` returns every live
  variant's source markdown.
- **Proposal endpoints require a `gc_u_` user token** (the same one
  `duck submit` uses): create, update, list-mine, get, withdraw.
  Reviewing and publishing are web-only.
- The agent write surface is gone and pinned gone by tests.

### CLI

- `duck propose` creates or updates a proposal; the educator sidecar
  remembers the proposal id and server. An explicit `--base` beats the
  sidecar (pointing at a different server drops the remembered id). The
  stale-sidecar and duplicate-proposal cases both recover automatically
  into the right open proposal.
- `duck proposals [status <id>]` tracks review progress.
- `duck educator push` is retired and says so; `educator pull` no longer
  needs a token.

### Sync flip: courses/ becomes a mirror

- `.github/workflows/course-sync.yml` pulls `/api/v1/export` every 6
  hours (and on dispatch) and maintains a **single auto-merging PR** on a
  fixed branch refreshing `courses/*.md`. Content was already
  human-reviewed in the app; CI's ingest tests re-verify every document
  parses before auto-merge.
- CD's publish step is removed. `make seed` imports straight into the
  local compose DB (pinned to the compose URL so an exported
  `DATABASE_URL` can't redirect it); `make import-courses-prod` is the
  documented break-glass import for bootstrap/disaster recovery.
- The sync loop is hardened against its failure modes: the PR-exists
  check filters to **open** PRs (`gh pr view` resolves merged PRs and
  would brick every sync after the first merge), drift detection stages
  first so brand-new course files sync, a concurrency group prevents
  overlapping runs, `jq -e` makes schema drift fail loudly, a
  zero-variant response refuses to empty the mirror, and exported
  course/language slugs are regex-checked before becoming filenames.

### Course/variant deletion

Deletion has **no HTTP surface at all** anymore — `store.DeleteCourse` /
`DeleteVariant` exist but are reachable only via psql, by design. An
admin delete UI was deliberately deferred.

## Alternatives considered

- **Keep the agent publish API alongside review**: two publish paths with
  different trust models; the credential (a bearer key with full write
  power) is exactly what the workflow exists to remove. Rejected.
- **GitHub PRs against `courses/` as the review medium** (repo stays the
  source of truth): requires reviewers to have GitHub accounts, hides
  review from the site, and leaves the DB permanently a derived artifact
  — wrong direction for an in-app community. Rejected; the repo keeps a
  read-only mirror for visibility, diffing, and disaster recovery.
- **Wiki-style direct edits with versioned rollback**: simplest UX but no
  quality gate at all; the review threshold *is* the product decision.
  Rejected.
- **Store-decided publishing** (AddReview publishes when the count is
  reached): couples deployment config into the store and makes the
  threshold untestable at the handler level. Rejected — the store reports,
  the web layer decides.

## Consequences

**Positive**
- Content governance matches the project's intent: humans propose, the
  community reviews in-app, admins can bootstrap. No standing write
  credential exists for course content.
- The DB is the single source of truth; the mirror is reproducible from
  `/api/v1/export` at any time, credential-free.
- Learner data is provably safe across republishes (same slug-diffing
  upsert path as before, now exercised by the proposal tests too).
- The review UI got a dependency-free patience line-diff
  (`internal/diff`) that handles 16k-line course docs in ~10 ms.

**Negative / accepted risks**
- **Auto-merged repo writes from server content**: course-sync pushes
  with a fine-grained PAT and auto-merges. Mitigations: content only ever
  lands in regex-validated `courses/*.md` paths, PR/commit strings are
  fixed, CI must pass, and the app-side ingest validation bounds what the
  server can export. Accepted.
- A fresh deploy needs manual bootstrap: `make import-courses-prod`, mint
  the first admin, optionally set `GC_APPROVAL_THRESHOLD`, delete the
  now-unused `GC_API_KEY` secret, and enable repo auto-merge (README /
  PR #87 checklist).
- Public `/api/v1/export` and the proposal validation pipeline have **no
  rate limiting or caching yet** — tracked in issue #89 with a DoD.
- Proposal review history keeps only each reviewer's latest verdict per
  proposal (upsert), not a full verdict audit trail. Fine at this scale.
- `revision` invalidation means a proposer editing anything (even a typo)
  resets approvals — safe by construction, occasionally annoying. The
  alternative (carrying approvals across revisions) was rejected as
  approving content reviewers never saw.
