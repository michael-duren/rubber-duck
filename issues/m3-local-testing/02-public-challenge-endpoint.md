# Public read endpoint for challenge starter + test code

## Context

Local test runs require handing users the test code. Tests aren't secret on
a learning platform; there is currently no unauthenticated way to fetch
them (the agent API needs a key, the web UI shows only prompt + starter).

## Work

- `GET /api/v1/courses/{slug}/variants/{language}/challenges` — public
  (no bearer key): JSON list of {lesson_slug, slug, title, points,
  starter_code, test_code}, including the final challenge.
- Reuse `store.VariantDetail` (internal/store/variant_detail.go); no new
  queries needed.

## Done when

`curl` with no auth returns the seed course's three challenges with starter
and test code.
