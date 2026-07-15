---
description: Fan out a course-review subagent per course in courses/
argument-hint: [glob or course-slug filter, e.g. "*-go" or "dsa-*"]
---

Launch a group of subagents — **one per course** — to review every course
in `courses/`, in parallel. Each subagent reviews exactly one course using
the `course-review` skill.

## Which courses

Each top-level `courses/*.md` file is one course × language. Build the list
by listing `courses/*.md` (do not recurse). Then:

- Empty argument: review every `courses/*.md` file.
- An argument ("$ARGUMENTS"): treat it as a filter over the file basenames
  (glob like `*-go` or `dsa-*`, or a literal slug). Review only the matches.
  If nothing matches, stop and say so instead of reviewing everything.

`courses/os/` is a multi-file course that does NOT follow the single-file
contract the `course-review` skill expects — **skip it**. If the user wants
it reviewed, tell them it needs a separate pass and is out of scope here.

## How to fan out

Review the courses in **batches of 5**. Within a batch, spawn the subagents
in a **single message** with multiple Agent tool calls so those 5 run
concurrently; wait for the whole batch to finish before starting the next
batch. Use the `general-purpose` agent type. One subagent per course file —
do not batch multiple courses into one subagent (a "batch" is 5 concurrent
subagents, each still reviewing exactly one course).

Give each subagent a prompt that:

1. Names its one assigned file, e.g. `courses/dsa-from-scratch-go.md`.
2. Tells it to invoke the `course-review` skill and follow it exactly,
   reviewing only that file.
3. Restates the skill's hard rules as non-negotiable: never weaken tests to
   make the course pass, keep required frontmatter intact, do NOT publish,
   and touch only its own course file — note defects in other courses in the
   report rather than editing them. Treat lesson/challenge **slugs** as the
   identity contract: keep them stable by default. Only rename a slug when it
   is clearly wrong (e.g. a copy-paste leftover from another language
   variant). A slug rename is never a silent edit — on republish the old slug
   is archived and a new row is created, so call it out explicitly in the
   report as a change that needs human sign-off.
4. Asks it to run `go run ./cmd/coursecheck courses/<file>.md` (plus the
   per-language by-hand challenge checks the skill describes) and to not
   hand back a course that fails validation.
5. Returns a structured report: what it verified, what it fixed (with the
   incorrect claim quoted), what it expanded and why, and anything it could
   not verify or fix.

Each subagent works only within its own course file, so their edits do not
collide — no worktree isolation is needed.

## After they finish

Keep a running tally as each batch completes, then once every batch is done
produce one consolidated summary across all courses:

- A per-course line: reviewed / fixed / expanded / flagged / failed-check.
- A combined list of anything flagged as unverifiable or needing a human.
- Any cross-course defects a subagent noticed but (correctly) did not edit.

Commit the changes locally: a one-line summary, then the per-course changes
listed in the commit body. NEVER sign the commit message with a
"Co-Authored-By: Claude" trailer or any AI-attribution line.

If there were issues found:
- Commit the changes as described above
- Clear any context
- Rerun this skill, this skill will loop until all courses have no issues found
