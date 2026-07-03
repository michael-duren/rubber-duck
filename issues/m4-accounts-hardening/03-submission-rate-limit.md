# Rate-limit submissions per user

## Context

Each submission spins a Cloud Run Job execution (costs money, takes a
worker slot). Nothing stops a user from hammering the submit button or a
script from queueing thousands.

## Work

- Per-user limit, e.g. max 3 in-flight (pending/running) submissions and a
  short cooldown between submits to the same challenge; one SQL count in
  the submit handler is enough — no in-memory limiter needed at this scale.
- Friendly error on the challenge page when over the limit.

## Done when

A burst of submits gets 429-style feedback and only the allowed number of
job executions are created.
