# Proportional scoring from per-test results

## Context

Scoring is all-or-nothing on the suite exit code. The schema already
carries `tests_passed`/`tests_total` columns and `grader.Result` has the
fields — only the parsing and score math are missing (deliberate M1 cut).

## Work

- Parse test counts per language in the runners or grader (go: `go test
  -json` events; pytest: `-q` summary line or `--junitxml`).
- Score = round(points * passed/total) with full points only on all-pass;
  keep exit-code fallback when parsing fails.
- Show passed/total on the submission result fragment.

## Done when

A half-passing submission earns partial points and displays e.g. "3/6
tests".
