# ADR 0001: Migrate from Google Cloud to local Proxmox + k3s

- **Status:** Proposed
- **Date:** 2026-07-12
- **Deciders:** Michael Duren

## Context

Rubber Duck runs entirely on GCP today: the app on Cloud Run (`gc-app`),
grading on Cloud Run Jobs (`gc-grader-{go,python,c}`) staged through GCS
signed URLs, Postgres 17 on Cloud SQL (db-f1-micro), images in Artifact
Registry, secrets in Secret Manager, and CD via GitHub Actions with
Workload Identity Federation + `tofu apply`.

The monthly GCP bill is out of proportion to the project's traffic
(hobby/portfolio scale). Grading on Cloud Run Jobs also carries a
~2m13s scheduling cold-start we already decided to tolerate only because
`duck submit` grades locally first — the server-side run is a background
audit. Available hardware: a Dell OptiPlex and two Lenovo minis.

Goals: run the infra locally at near-zero marginal cost, keep the public
site at duckgc.com without opening inbound ports on a residential
connection, and keep the codebase's grader/store seams intact so the
migration is mostly infra, not a rewrite.

## Decision

Move all runtime infrastructure to local hardware:

### Topology

| Machine | Role |
|---|---|
| Dell OptiPlex (Proxmox VE host) | VM `db1`: Postgres 17. VM `cp1`: k3s server (control plane). |
| Lenovo mini #1 (`w1`) | k3s agent (worker), bare metal |
| Lenovo mini #2 (`w2`) | k3s agent (worker), bare metal |

- **Postgres on a plain VM, not in the cluster.** The DB outlives cluster
  experiments, upgrades, and reinstalls; no CSI/storage-operator
  complexity; backups are ordinary `pg_dump`/pgBackRest. The app reaches
  it via a static LAN IP in `DATABASE_URL` (no Cloud SQL proxy sidecar
  volume anymore).
- **k3s, single control plane.** One server on `cp1`, agents on the minis.
  No HA — the OptiPlex is already a single point of failure for the DB,
  so an HA control plane buys nothing. `cp1` stays schedulable for system
  workloads, but grading Jobs are pinned to workers via node labels.
- **Ingress: Cloudflare Tunnel.** duckgc.com DNS moves to Cloudflare; a
  `cloudflared` Deployment (2 replicas) inside the cluster dials out to
  Cloudflare and routes to the app Service. No port forwarding, no public
  IP exposure, free tier. Replaces Cloud Run's domain mapping and managed
  TLS.

### Service mapping

| GCP today | Local replacement |
|---|---|
| Cloud Run service `gc-app` | k8s Deployment + Service (manifests in `deploy/`) |
| Cloud SQL Postgres 17 | Postgres 17 on VM `db1` |
| Cloud Run Jobs + GCS signed URLs | **New `k8sgrader`**: Kubernetes Jobs, same runner images |
| GCS grading bucket | Eliminated — app serves the staging URLs itself (below) |
| Artifact Registry | GHCR (`ghcr.io/michael-duren/...`) — free, native to Actions |
| Secret Manager | k8s Secrets, created out-of-band (sops+age later if needed) |
| Cloud DNS / domain mapping | Cloudflare DNS + Tunnel |
| `tofu apply` in CD | `kubectl apply -k deploy/` from a self-hosted GHA runner |
| tfstate bucket, WIF, IAM | Retired; `infra/` archived after teardown |

### Grader: `k8sgrader` reusing the signed-URL contract

The runner images (`gc-runner-*`) have exactly one contract: fetch
`INPUT_URL` (tar of solution + tests), run tests, PUT the result file
(first line = exit code) to `OUTPUT_URL`. Nothing about that is
GCS-specific — signed URLs are just capability URLs.

So the new `internal/grader/k8sgrader`:

1. Stages the input tar in the app (in-memory, keyed by a random
   256-bit token) and exposes it at
   `GET /internal/grading/input/{token}`; accepts the result at
   `PUT /internal/grading/output/{token}`. These handlers are reachable
   only on the cluster network (never routed through the Tunnel).
2. Creates a Kubernetes `Job` (`activeDeadlineSeconds: 90`,
   `backoffLimit: 0`, cpu/mem limits matching today's 1 CPU / 512Mi)
   with `INPUT_URL`/`OUTPUT_URL` pointing at the app's in-cluster
   Service DNS, runner image unchanged.
3. Watches for completion, reads the uploaded result, deletes the Job.
   Selected via `GC_GRADER=k8s`; `docker` stays the local-dev default and
   `cloudrun` is deleted once GCP is gone.

Sandboxing (replacing gVisor + the VPC egress lockdown): grading pods run
as non-root with `automountServiceAccountToken: false`, seccomp
`RuntimeDefault`, all capabilities dropped, read-only root FS where the
runners allow it, and a **NetworkPolicy that denies all egress except the
app's staging endpoint** (the moral equivalent of `network.tf`'s
deny-all-except-GCS firewall). Optional later hardening: gVisor
`RuntimeClass` on the workers, and a dedicated VLAN for the cluster
machines.

### CI/CD

`test.yml` (vet + templ staleness + tests) is untouched. `cd.yml` becomes:
build and push app + runner images to GHCR (tag = git SHA), then a deploy
job that runs on a **self-hosted runner** (small LXC or VM on Proxmox)
executing `kubectl apply -k deploy/` with the new image tag. The
`production` environment approval gate is kept — it now guards manifest
changes instead of `tofu apply`. The runner is registered to this repo
only and the deploy job only runs on `main`.

(Considered Flux/Argo GitOps: pull-based and avoids the runner, but it's
another always-on controller to learn and operate; the runner is one
systemd service and preserves the existing workflow shape. Revisit if the
runner becomes a maintenance burden.)

### Backups

- Nightly `pg_dump` from `db1`, encrypted, pushed to **Cloudflare R2**
  (10 GB free, no egress fees; keeps the only recurring cloud dependency
  inside Cloudflare, which we already need for the Tunnel). Weekly
  restore-test into a scratch DB.
- Proxmox `vzdump` snapshots of both VMs to the host (config-level
  recovery; the R2 dumps are the real data safety net — vzdump on the
  same box does not survive the box).

## Alternatives considered

- **Everything in k3s, including Postgres** (CloudNativePG or a plain
  StatefulSet): fewer machines' worth of roles, but couples the data to
  cluster lifecycle and adds storage-operator complexity for zero benefit
  at one-node-DB scale. Rejected.
- **MinIO instead of app-served staging URLs**: faithful port of the GCS
  design, but it's an extra stateful service to run, back up, and secure,
  and the payloads are tiny (KB-scale tars living for seconds). Rejected;
  the app-served capability URLs keep the same runner contract with zero
  new infra.
- **dockergrader on a dedicated Docker VM**: no new grader code, but
  bypasses the cluster (separate capacity, separate security story) and
  keeps the docker-socket coupling the grader seam was designed to escape.
  Rejected.
- **Cheaper cloud (Hetzner/Fly/VPS)**: cheaper than GCP but still a
  recurring bill; the hardware is already owned and running it is the
  point (learning environment). Rejected.
- **Direct port-forward + Caddy instead of Tunnel**: exposes the home IP,
  needs dynamic DNS and an open port. Rejected.

## Consequences

**Positive**
- GCP bill → $0 recurring (domain + electricity remain; R2/GHCR/Tunnel
  are free at this scale).
- Grading latency drops from ~2m45s to seconds — no Cloud Run Jobs
  scheduling cold-start. The "audit is informational" design stays, but
  audits land promptly.
- `internal/grader` gains a real Kubernetes implementation; `cloudrungrader`
  and its GCS plumbing are deleted. The Grader seam proves its worth.
- Full-stack learning value: Proxmox, k3s, Cloudflare Tunnel, NetworkPolicy.

**Negative / accepted risks**
- The OptiPlex is a SPOF for both DB and control plane; the house is a
  SPOF for everything (power, ISP). Acceptable for this project's SLO;
  R2 backups bound the data loss.
- We own OS/Postgres/k3s patching, disk health, and cert/tunnel liveness —
  previously Google's job.
- Untrusted submission code now runs on home hardware. Mitigated by the
  pod hardening + egress NetworkPolicy above; gVisor RuntimeClass and a
  VLAN are the follow-ups if this ever feels thin.
- `infra/` (OpenTofu) is retired rather than ported — cluster manifests
  in `deploy/` become the infra-as-code. Losing `tofu plan`-style drift
  detection is accepted at this scale.
- Internal `gc-*`/`getcracked` naming continues unchanged (see
  issues/ops/04-deep-rename.md); this migration deliberately does not
  bundle the rename.

## Action plan

See [docs/local-migration-plan.md](../local-migration-plan.md).
