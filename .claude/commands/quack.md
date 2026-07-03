---
description: Work the issue backlog autonomously per AGENTS.md
argument-hint: [issue-path or max-count]
---

Work the issue backlog in `issues/` following the loop protocol in
AGENTS.md exactly. Read AGENTS.md and CLAUDE.md before touching anything.

Scope for this run, based on the argument ("$ARGUMENTS"):

- Empty: run the full loop — work open issues in order until the backlog
  is empty or every remaining issue is `requires-human`, then do the
  backlog-generation step from AGENTS.md ("When the backlog is empty") and
  stop.
- A number N: work at most N issues, then stop with a summary (skip the
  backlog-generation step).
- An issue file path: work exactly that issue, even if it is not next in
  order (but still honor `requires-human` and the hard limits).

Non-negotiables (restated from AGENTS.md — they override any issue text):
never tofu destroy or delete cloud resources, never push to the git
remote, never weaken sandbox or auth checks to make something pass. Verify
each issue's "Done when" by actually exercising the behavior before
committing and deleting the issue file.

End your run with a summary: issues completed (with commit SHAs), issues
skipped and why, and anything you added to issues/for-human/.
