# Publish a Python variant of the seed course

## Context

The python grading path (gc-runner-python image, gc-grader-python job) is
built and deployed but has never graded a real submission. The spec calls
for courses translated across languages; there is no python content yet.

## Work

- Author `seed/intro-to-concurrency-python.md` — same course slug
  `intro-to-concurrency`, `language: python`, same lesson/challenge slugs,
  content translated (threading/queue in place of goroutines/channels).
- Tests import from `solution` (e.g. `from solution import merge`).
- Seed to prod; submit pass/fail solutions to verify the python job path.

## Done when

The course page shows a language picker with go + python and a python
submission grades correctly in production.
