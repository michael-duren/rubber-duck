# Reduce server-side grading latency

## Context

Production grading takes 30-60s, almost all Cloud Run Job scheduling +
container cold start. The gc CLI (m3) removes the iteration pain; this
issue is about the submit-for-points path itself.

## Options to evaluate

- Slim the runner images (go image is large; multi-stage with only the
  toolchain layers needed).
- Pre-created execution pool or Cloud Run *service* graders with
  min-instances (trade: always-on cost).
- Measure first: log stage timings (stage upload, RunJob start, execution
  duration) before choosing.

## Done when

P50 submit-to-verdict is under ~15s, or a documented decision that current
latency is acceptable given the CLI workflow.
