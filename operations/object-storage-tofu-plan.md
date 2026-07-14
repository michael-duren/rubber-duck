# Plan: object storage (Garage) under OpenTofu

Status: **draft / not yet implemented.** Companion to
[garage-setup.md](garage-setup.md), which covers doing it by hand first —
do a manual pass once so the tofu code is encoding something you've seen
work, not a guess.

## Where this fits

`infra/` is today's GCP root module (Cloud Run, Cloud SQL, GCS). The
homelab is a different lifecycle with different credentials, so it gets a
**separate root module** rather than more resources in the GCP one:

```
infra/            # existing GCP module — untouched until decommission
infra/homelab/    # new root: k3s-hosted pieces (garage first, app later)
```

Same conventions as `infra/`: `tofu` CLI (never terraform),
`versions.tf` pinning providers, `make infra-validate` extended to cover
both roots. State: local file to start (it's a homelab; commit an
encrypted copy or keep it on the NAS/backup path), promotable to an
S3 backend *in Garage itself* later — bootstrap order matters: Garage
can't hold the state of the module that creates Garage on first apply.

## Layers (apply order)

**Layer 0 — Proxmox VMs (optional, later).** The `bpg/proxmox` provider
can manage the VMs themselves (k3s nodes, the Garage/Postgres VM). Skip
initially — the VMs already exist; import them if/when drift becomes
annoying.

**Layer 1 — Garage deployment.** Two shapes; the plan prefers (a) to
start, matching garage-setup.md:

- (a) *VM systemd service*: tofu's job shrinks to config + secrets
  templating. Practical: `local_file` / `remote-exec` is clunky — honest
  answer is garage-setup.md by hand (or ansible if the itch grows), and
  tofu starts at Layer 2. Zero new providers.
- (b) *In-cluster via Helm chart*: `helm_release` of the chart vendored
  from the Garage repo (`script/helm/garage` — vendor it; there's no
  stable published chart registry), `kubernetes` + `helm` providers
  pointed at the k3s kubeconfig, PVC on `local-path`, node-pinned via
  nodeSelector to the storage node. This is the end-state shape once
  the cluster itself feels boring.

```hcl
# providers.tf (homelab root)
provider "kubernetes" {
  config_path = var.kubeconfig_path   # k3s: /etc/rancher/k3s/k3s.yaml copied out
}
provider "helm" {
  kubernetes { config_path = var.kubeconfig_path }
}
```

**Layer 2 — Garage resources (bucket, key, grants).** Garage has an
admin HTTP API (port 3903) covering layout, buckets, keys, grants.
Options, in order of preference:

1. A community `garage` tofu provider exists on the registry — evaluate
   it (pin it hard; homelab-grade providers churn). Verify it covers
   bucket + key + allow before adopting.
2. Fallback that always works: a small idempotent bootstrap script
   (curl against the admin API, or `garage` CLI over SSH) run as a
   `terraform_data`/`null_resource` with triggers on the desired state —
   ugly but explicit, and the surface is 3 calls (create bucket, create
   key, allow).

Either way the *secret key* lands in tofu state — acceptable for local
state on the homelab, another reason not to put this state in a public
place.

**Layer 3 — app wiring.** The app (still on Cloud Run during
transition, later in k3s) consumes:

```
S3_ENDPOINT / S3_REGION / S3_BUCKET / S3_ACCESS_KEY_ID /
S3_SECRET_ACCESS_KEY / S3_FORCE_PATH_STYLE
```

- While on GCP: add these as Secret Manager entries in the existing
  `infra/` module fed from Layer-2 outputs (manual copy or
  `terraform_remote_state`).
- On k3s: a `kubernetes_secret` in the homelab root, mounted as env.

**Layer 4 — data migration + decommission.** Not tofu, but sequenced
here: `rclone sync` GCS → Garage for anything not reproducible; course
content re-renders from `courses/*.md` via `make publish`, so the real
migration is mostly grader-staging (ephemeral, can start empty) and any
future course assets. Then GCS resources get removed from `infra/` last,
after prod traffic proves the Garage path.

## Code-side prerequisite (separate PR, before any of this matters)

Promote `objectStore` out of `cloudrungrader` into `internal/blob`
(interface where consumed, `gcs` + `s3` implementations, selected by
config). aws-sdk-go-v2 with `BaseEndpoint`, `UsePathStyle: true`,
`s3.NewPresignClient` for signed URLs. Until this lands, nothing above
has a consumer.

## Open questions

- **"Indexing will have to get creative"** — once course *content* lives
  in object storage with only metadata in SQL, catalog search
  (`matchesQuery`, tag filtering) still works off SQL metadata, but
  full-text over lesson bodies won't. Options when we get there:
  keep rendered-HTML + a tsvector column in Postgres as a search index
  (content in S3, index in SQL — no new infra), or something like
  Meilisearch as another k3s workload. Decide when content actually
  moves; don't build it speculatively.
- Does grader staging even need S3 at home? If runner jobs execute in
  the same k3s cluster as the app, the dockergrader model (tar over
  stdin, no object store) may serve better than porting the
  signed-URL flow. Decide before building `internal/blob/s3` presign
  support nobody calls.
- k3s app deployment itself (replacing Cloud Run) is its own plan —
  image registry, ingress, cert-manager, Postgres move. This document
  deliberately covers only object storage.

## Definition of done (for this plan)

1. `infra/homelab/` root exists, `tofu validate` green in CI alongside
   `infra/`.
2. Garage reachable at a TLS endpoint with bucket+key provisioned by
   Layer 2, reproducible from scratch.
3. App (wherever it runs) does a full grade cycle with `S3_ENDPOINT`
   pointed at Garage in a staging config.
4. Backups (garage-setup.md §6) running on a timer before any prod
   cutover.
