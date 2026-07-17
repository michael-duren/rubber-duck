---
name: course-review
description: >
  Review a course markdown file in courses/ for technical accuracy and
  learner clarity, expanding thin or confusing sections. Use when asked to
  review, audit, improve, or fact-check a course, or after authoring a new
  course variant and before opening its PR.
---

# Course review

You are reviewing a Rubber Duck course for two things, in this order:

1. **Accuracy** — every technical claim, code sample, number, and API name
   is correct and current.
2. **Learner clarity** — a motivated learner meeting this material for the
   first time can follow it without outside help. Expand where they can't.

The course format contract is `internal/ingest/parse.go`, documented in
README.md ("Course document format"). One file per course × language in
`courses/<course-slug>-<language>.md`.

## Workflow

1. **Read the whole course start to finish** before editing anything.
   Judge it as a learner would: does each lesson build only on what came
   before? Is every new term defined at first use? Does each challenge test
   what its lesson actually taught?

2. **Verify accuracy claim by claim.** Do not trust prose on sight:
   - Run every runnable code sample and worked example. If a lesson shows
     concrete output (hash values, benchmark numbers, bucket distributions,
     goroutine interleavings), reproduce it in the scratchpad and fix the
     text if it doesn't match.
   - Check factual claims about languages, libraries, standards, and
     history against authoritative sources (language specs, official docs)
     when in doubt.
   - Check challenge coherence: the Starter compiles/imports cleanly, the
     Tests actually exercise what the prompt asks for, and a correct
     solution to the prompt passes the Tests. Tests must be self-contained
     and stdlib-only (see README for per-language test conventions).

3. **Expand what's thin.** Signs a section needs expansion:
   - A concept is used before it's explained, or explained only by naming it.
   - A "why" is missing — the text says *what* to do but a learner couldn't
     say why it works or when it fails. This platform's house style is to
     teach the why (see existing courses for tone).
   - A jump between two steps that took the author one thought but will
     take a learner ten (add the intermediate step or a worked example).
   - A challenge whose prompt assumes knowledge the lesson never covered.

   When expanding, match the course's existing voice and density. Prefer
   concrete worked examples (with real, verified values) over more
   abstract prose. Do not pad — if a section is clear and correct, leave
   it alone.

4. **Validate before finishing.** Run the offline checker — it uses the
   same parser as the ingest API:

   ```sh
   go run ./cmd/coursecheck courses/<file>.md
   # cpp only: also compile-checks challenges; with reference solutions:
   go run ./cmd/coursecheck -solutions <dir> courses/<file>.md
   ```

   `coursecheck` compile-checks challenges only for `cpp` courses. For
   go/python/c, verify challenges by hand: copy each Starter and Tests
   pair into the scratchpad and run the language's toolchain the way the
   grader does (`go test ./...` in package `challenge`; `pytest` importing
   from `solution`; `cc solution.c test_solution.c && ./a.out`). The
   Starter must compile (stub bodies are fine); tests should fail on the
   bare starter and pass on a correct solution.

   Fix every error; do not hand off a course that fails validation.

5. **Report.** Summarize what you verified, what you fixed (with the
   incorrect claim quoted), and what you expanded and why. Flag anything
   you could not verify rather than silently letting it stand.

## Hard rules

- **Never change a lesson or challenge slug** (`{#slug}`). Slugs identify
  content across re-publishes; changing one deletes learner submissions on
  the next publish. Retitle freely — slugs stay.
- **Never weaken tests to make a course pass review.** If tests and prose
  disagree, work out which is right and fix the wrong one.
- Keep required frontmatter intact: `course`, `title`, `language`,
  `description`. `language` must match the filename suffix.
- Don't publish. `courses/*.md` is a mirror of the server; content changes
  go through the in-app proposal workflow (web editor or `duck propose`),
  so land improvements as a proposal, not a direct edit to the mirror.
- One course per review pass. If you notice a defect in a *different*
  course while reviewing, note it in your report instead of editing it.
