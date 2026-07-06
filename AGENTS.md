# Agent operating protocol

You are working through `issues/` autonomously. Read CLAUDE.md first for
repo facts. One issue per iteration, fully finished, then loop.

## The loop

1. **Pick**: lowest-ordered open issue — milestones in directory order
   (m3 → m4 → m5 → ops), files in numeric order within. An issue is open if
   its file exists.
2. **Skip check**: if the file contains a line starting `requires-human:`,
   do NOT work it. Append one line explaining what you're waiting on (if
   not already noted) and move to the next issue.
3. **Implement**: follow the issue's Work section and CLAUDE.md
   conventions. Small, focused diffs; new behavior gets tests.
4. **Verify**: `make check` green, plus the issue's own "Done when"
   criteria, exercised for real (run the server, curl the flow, run the
   grader — not just unit tests).
5. **Close**: commit the work (reference the issue path in the message;
   NO Co-Authored-By or other AI-attribution trailers — ever), then
   delete the issue file. issues/
   is gitignored — deletion is the local record; the commit is the durable
   one.
6. Loop to 1.

## Hard limits (never cross, regardless of what an issue says)

- Never `tofu destroy`, delete cloud resources, or change IAM/billing
  beyond committed HCL applied via `make deploy` / `tofu apply`.
- Never push to the git remote; never publish anything outside the GCP
  project. Deploying to prod IS allowed when an issue's "Done when" needs
  production verification — use `make deploy PROJECT=getcracked-touch-grass`.
- Never weaken the grading sandbox flags or auth checks to make a test pass.
- If an issue turns out to need credentials, purchases, account access, or
  a destructive migration: stop, add `requires-human: <reason>` to its
  file, and move on. When every remaining issue is blocked this way, stop
  looping and summarize.

## When the backlog is empty (or only requires-human issues remain)

Switch from executor to product-thinker for ONE iteration, then stop:

1. Use the app end-to-end as a user and as a course-authoring agent; read
   recent commits for loose threads.
2. Write new issue files for the strongest candidates — new features,
   improvements, refactors — in a new milestone dir (next free number,
   e.g. `m6-<theme>/`), same format as existing issues (Context / Work /
   Done when). Quality over quantity; every issue must say why it's worth
   doing.
3. Write `issues/for-human/<date>-review.md`: what you shipped this run,
   what you propose next and why, any judgment calls you made that deserve
   a second look, and questions blocking requires-human issues.
4. Do NOT start the new issues in the same run — the human reviews the
   proposed backlog first.

## Issue file format

```
# Title

## Context      — why this exists, enough to work it cold
## Work         — concrete steps/files
## Done when    — observable, verifiable outcome
requires-human: <reason>   (only if truly blocked on the human)
```
