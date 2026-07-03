# Restrict grading job network egress

## Context

Documented M2 tradeoff: grading jobs have open internet egress because the
runner curls GCS signed URLs. gVisor + a zero-role service account limit
blast radius, but submission code can still exfiltrate/download freely.

## Work

- VPC connector for the jobs + egress=all-traffic through the VPC, firewall
  allowing only storage.googleapis.com (Private Google Access), or
  restricted.googleapis.com routing.
- Verify both grading modes still work; measure added cold-start cost.

## Done when

A submission that curls an arbitrary external host fails while grading
still passes.
