# Local migration action plan

Executes [ADR 0001](adr/0001-migrate-gcp-to-local-k3s.md). Phases 1–6 can
proceed while prod stays on GCP; phase 7 is the cutover window; phase 8
is teardown. Nothing before phase 7 touches the live site.

## Phase 0 — Prep (no hardware needed)

- [ ] Move duckgc.com nameservers to Cloudflare (free plan). DNS keeps
      resolving to GCP during the transfer; the Tunnel comes later.
- [ ] Create a Cloudflare R2 bucket for DB backups + an API token scoped
      to it.
- [ ] Make GHCR available: confirm repo package permissions, decide
      public vs private images (private needs an imagePullSecret in k3s).
- [ ] Inventory the minis + OptiPlex: RAM/disk, enable WoL/auto-power-on
      after outage in BIOS, wire everything to the router/switch on
      ethernet with DHCP reservations (static IPs).

## Phase 1 — Proxmox on the OptiPlex

- [ ] Install Proxmox VE on the OptiPlex (ZFS single-disk or ext4; enable
      the no-subscription apt repo).
- [ ] Create VM `db1` (Debian 12, 2 vCPU / 4 GB / 40 GB) and VM `cp1`
      (Debian 12, 2 vCPU / 4 GB / 30 GB). Static IPs, SSH keys only.
- [ ] Set up `vzdump` scheduled backups for both VMs on the host.

## Phase 2 — Postgres on `db1`

- [ ] Install Postgres 17 (PGDG repo), listen on the LAN address,
      `pg_hba.conf` scoped to the cluster subnet, dedicated `duck` role +
      database. TLS optional on a trusted LAN — decide and note it.
- [ ] Nightly `pg_dump` → age-encrypt → `rclone` to R2 (systemd timer).
      14-day retention. Script + unit files live in `deploy/db/`.
- [ ] **Rehearse the real migration now**: `bin/cloud-sql-proxy` →
      `pg_dump` prod → restore into `db1` → run the app locally against it
      (`DATABASE_URL=...db1...` `make dev` without compose Postgres) and
      click around. This validates dump/restore long before cutover.
- [ ] Verify a restore from the R2 copy, not just the local dump.

## Phase 3 — k3s cluster

- [ ] k3s server on `cp1` (`--disable traefik` — no ingress controller
      needed; the Tunnel terminates routing). Agents on `w1`/`w2` with the
      node token. Label workers `duck.gc/grading=true`.
- [ ] Copy kubeconfig to the laptop; sanity-check `kubectl get nodes`.
- [ ] Smoke test: run a busybox Job pinned to a worker; confirm
      NetworkPolicy enforcement works with a deny-all test policy
      (k3s's default flannel does NOT enforce NetworkPolicy via the CNI —
      k3s ships kube-router's netpol controller; verify it's active).

## Phase 4 — App manifests (`deploy/`)

- [ ] Write kustomize manifests: Namespace `duck`, app Deployment
      (2 replicas, spread across workers), Service, Secret for
      `DATABASE_URL` (created out-of-band, documented in deploy/README),
      RBAC ServiceAccount for the app limited to Jobs create/get/watch/
      delete + result reads in its own namespace.
- [ ] Push app + runner images to GHCR from a laptop build first
      (`make` target), deploy, and reach the app on the LAN via
      `kubectl port-forward`. Grading isn't wired yet (k8sgrader is
      phase 5) — that's fine: submissions queue and the pool drains them
      once a grader exists.

## Phase 5 — `k8sgrader` (the only real code work)

- [ ] `internal/grader/k8sgrader`: create Job with runner image,
      `INPUT_URL`/`OUTPUT_URL` env pointing at the app Service, watch to
      completion, collect result, delete Job. Table tests with a fake
      clientset mirroring `cloudrungrader`'s test structure.
- [ ] Staging handlers in `internal/web` (or a small `internal/staging`):
      `GET /internal/grading/input/{token}`, `PUT /internal/grading/output/{token}`,
      random 256-bit tokens, single-use, TTL'd. Never routed via the Tunnel
      (Tunnel config only exposes `/` on the public hostname; also guard
      with a cluster-CIDR check).
- [ ] Wire `GC_GRADER=k8s` in `cmd/duckserver/main.go` (needs in-cluster
      config + namespace + image tag envs). Keep `cloudrun` until teardown.
- [ ] Grading Job pod hardening: runAsNonRoot, no SA token, seccomp
      RuntimeDefault, drop all caps, limits 1 CPU/512Mi,
      `activeDeadlineSeconds: 90`, nodeSelector `duck.gc/grading=true`,
      plus the deny-all-egress-except-app NetworkPolicy.
- [ ] End-to-end on the cluster: submit through the browser for all three
      languages; confirm pass/fail/error paths and that Jobs get cleaned up.
- [ ] `make check` green; PR it.

## Phase 6 — Cloudflare Tunnel + CI/CD

- [ ] Create a named tunnel; deploy `cloudflared` (2 replicas) in the
      cluster routing `duckgc.com` → app Service. Test via a temporary
      hostname (e.g. `staging.duckgc.com`) against the local stack —
      full public-path test **before** touching the real DNS record.
- [ ] Self-hosted GHA runner (small Proxmox LXC/VM, repo-scoped, labeled
      `duck-deploy`), kubeconfig with a deploy-only ServiceAccount.
- [ ] Rewrite `cd.yml`: tests → build/push to GHCR (SHA tag) →
      `production`-gated deploy job on the self-hosted runner running
      `kubectl apply -k deploy/` with the new tag. Remove WIF/GCP auth
      steps. Update `docs/infra.md` (the WIF/tfstate sections become
      historical).

## Phase 7 — Cutover (maintenance window, ~30 min)

- [ ] Lower duckgc.com TTL a day ahead (Cloudflare: just switch the
      record to the Tunnel when ready — proxied records cut over fast).
- [ ] Scale Cloud Run `gc-app` to 0 (stops writes) → final `pg_dump` via
      cloud-sql-proxy → restore into `db1` → verify row counts on key
      tables (users, submissions, courses).
- [ ] Point duckgc.com at the tunnel. Verify: login session survives
      (session table came over), course pages render, a real submission
      grades end-to-end, `duck` CLI + agent API key still work.
- [ ] Watch logs for a day. Rollback path: repoint DNS at Cloud Run and
      scale it back up — GCP stays intact until phase 8.

## Phase 8 — GCP teardown

- [ ] After a comfortable soak (1–2 weeks): download a final copy of the
      tfstate + last Cloud SQL dump for the archive.
- [ ] `tofu destroy` (needs the un-applied `network.tf` situation checked
      first — destroy plans against state, so it's fine, but review the
      plan). Delete the tfstate bucket, WIF pool, gh-deployer SA; disable
      billing on the project.
- [ ] Delete `internal/grader/cloudrungrader`, the `cloudrun` case in
      `main.go`, GCS deps from go.mod; archive `infra/` (git history keeps
      it — delete the directory, note it in the ADR status: Accepted).
- [ ] Update README/CLAUDE.md/AGENTS.md deploy sections (`make deploy`
      target changes meaning), remove `psql-prod`'s proxy flow in favor of
      `psql -h db1`.

## Phase 9 — Hardening & operations (post-migration backlog)

- [ ] Uptime monitoring: external ping on duckgc.com (Cloudflare health
      check or UptimeRobot free) + Uptime Kuma inside the LAN.
- [ ] gVisor `RuntimeClass` on workers for grading Jobs (restores the
      Cloud Run sandbox level).
- [ ] VLAN or firewall isolation for the cluster machines from the rest
      of the home LAN.
- [ ] Patching routine: unattended-upgrades on VMs/minis, periodic k3s
      and Proxmox updates; note it in docs/infra.md.
- [ ] Quarterly restore drill from R2.

## Open decisions (flag before the relevant phase)

1. **Public vs private GHCR images** (phase 0/4) — public is zero-config;
   private needs a pull secret. Runner images contain no secrets, so
   public is likely fine.
2. **Postgres TLS on the LAN** (phase 2) — skip on a trusted/isolated
   VLAN, or `sslmode=require` with a self-signed cert if the LAN is shared.
3. **Where the self-hosted runner lives** (phase 6) — Proxmox LXC is
   lightest; a VM is better isolated since it executes CI-defined code.
