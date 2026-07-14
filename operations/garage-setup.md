# Garage: self-hosted S3-compatible object storage

Garage (<https://garagehq.deuxfleurs.fr/>) is the object store for the
homelab migration: course content (and the grader's staging blobs, if
runners stay remote from storage) live in S3, while metadata and all user
data stay in Postgres.

Why Garage over the alternatives:

- Single static Rust binary, tiny footprint, built for self-hosters —
  happy on one node, scales to a few.
- Speaks enough S3 for us: V4 signatures, presigned GET/PUT URLs (what
  the grader flow uses), path-style addressing.
- No licensing drama: MinIO's community edition was progressively gutted
  through 2025; SeaweedFS and Ceph are multi-component systems that are
  overkill for two machines.

## Target topology

Two Proxmox hosts:

- **host A** — k3s control plane + database VMs
- **host B** — k3s workers only

Two sane deployment shapes; pick one:

1. **VM-level service (recommended to start).** Run Garage as a plain
   systemd service (or docker container) on a dedicated VM on host A,
   next to Postgres — i.e. treat it like the database it effectively is.
   Storage stays decoupled from k3s upgrades/reschedules, and the app in
   k3s just points at an endpoint. Simplest thing that works; this doc
   covers it first.
2. **In-cluster (k3s) via Garage's Helm chart.** A StatefulSet with a
   PersistentVolume pinned to one node. More moving parts, but everything
   is one declarative system — this is the shape the OpenTofu plan
   ([object-storage-tofu-plan.md](object-storage-tofu-plan.md)) targets
   long-term.

Replication: with two physical hosts you can either run **one node,
`replication_factor = 1`** and rely on backups (fine to start — Postgres
on this setup has the same story), or run **one Garage node per host with
`replication_factor = 2`** so either machine can die without losing
objects. Garage's docs prefer 3 nodes/zones, but 2 is supported. Start
with 1 + backups; add the second node when the workers host is stable.

## 1. Install (VM-level, single node)

Arch/manual (matches how you run things elsewhere; there's no official
Arch package — check the AUR for `garage`, or just grab the static
binary):

```sh
# static binary — check https://garagehq.deuxfleurs.fr/download/ for the
# current version and substitute it below
curl -fLo /usr/local/bin/garage \
  https://garagehq.deuxfleurs.fr/_releases/<version>/x86_64-unknown-linux-musl/garage
chmod +x /usr/local/bin/garage
```

Config at `/etc/garage.toml`:

```toml
metadata_dir = "/var/lib/garage/meta"
data_dir     = "/var/lib/garage/data"
db_engine    = "lmdb"

replication_factor = 1

# RPC is node-to-node traffic; keep it on the LAN interface.
rpc_bind_addr = "0.0.0.0:3901"
rpc_public_addr = "<vm-lan-ip>:3901"
rpc_secret = "<openssl rand -hex 32>"

[s3_api]
s3_region = "garage"          # arbitrary but must match client config
api_bind_addr = "0.0.0.0:3900"
root_domain = ".s3.home.duckgc.com"   # vhost-style; path-style works regardless

[admin]
api_bind_addr = "127.0.0.1:3903"
admin_token = "<openssl rand -hex 32>"
```

systemd unit `/etc/systemd/system/garage.service`:

```ini
[Unit]
Description=Garage object storage
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/garage server
Restart=on-failure
DynamicUser=true
StateDirectory=garage
Environment=GARAGE_CONFIG_FILE=/etc/garage.toml
# DynamicUser + StateDirectory puts state under /var/lib/garage owned by
# the transient user; point metadata_dir/data_dir there (as above).

[Install]
WantedBy=multi-user.target
```

```sh
systemctl enable --now garage
journalctl -u garage -f   # watch it come up
```

## 2. Cluster layout (even for one node)

Garage won't serve requests until every node is assigned a zone and
capacity — that's the "layout":

```sh
garage status                       # shows the node ID
garage layout assign <node-id> -z hostA -c 500G
garage layout apply --version 1
```

`-c` is a *relative weight*, not a quota — size it roughly to the disk
you're giving it. Adding the second node later is the same dance:
`garage node connect <id>@<hostB-ip>:3901`, `layout assign -z hostB`,
`layout apply` (and bump `replication_factor` to 2 first).

## 3. Bucket and key for Rubber Duck

```sh
garage bucket create rubber-duck-courses
garage bucket create rubber-duck-grading      # if/when grader staging moves here

garage key create rubber-duck-app
# prints: Key ID (GK...), Secret key — the secret is shown ONCE, like our
# API keys. Put it straight into the app's secret store, nowhere else.

garage bucket allow rubber-duck-courses --read --write --key rubber-duck-app
garage bucket allow rubber-duck-grading --read --write --key rubber-duck-app
```

Sanity check with any S3 client (path-style + custom endpoint):

```sh
aws --endpoint-url http://<vm-lan-ip>:3900 \
    --region garage \
    s3 ls s3://rubber-duck-courses   # with AWS_ACCESS_KEY_ID/SECRET set
```

## 4. TLS and endpoints

Garage itself serves plain HTTP; put TLS in front:

- **In-cluster consumers only** (app pods → Garage over the LAN): plain
  HTTP to the VM IP is acceptable inside the home network to start.
- **Presigned URLs** are the forcing function for real TLS + DNS: the
  hostname is baked into the signed URL, so whoever consumes the URL
  (browser, grader job) must be able to resolve and trust it. Terminate
  TLS at the k3s Traefik ingress (or a Caddy/nginx on the VM) for
  `s3.home.duckgc.com` → `:3900`, with a LAN DNS entry (or a public DNS
  record + Let's Encrypt DNS-01 challenge, no port-forward needed).
  Note: signatures cover the Host header — proxy with the hostname
  intact, don't rewrite it.

## 5. App integration

The seam already exists: `internal/grader/cloudrungrader/clients.go`
defines the `objectStore` interface (Put/Get/Delete/SignedGetURL/
SignedPutURL) with a GCS implementation. The migration adds an S3
implementation (aws-sdk-go-v2 with `BaseEndpoint` + `UsePathStyle: true`,
presigning via `s3.NewPresignClient`) selected by config:

```
S3_ENDPOINT=https://s3.home.duckgc.com
S3_REGION=garage
S3_BUCKET=rubber-duck-courses
S3_ACCESS_KEY_ID=GK...
S3_SECRET_ACCESS_KEY=...
S3_FORCE_PATH_STYLE=true
```

## 6. Backups

Object stores don't exempt you from backups — replication protects
against disk death, not `garage bucket delete` or a bad deploy. Two easy
options, either on a timer:

- `rclone sync garage:rubber-duck-courses /backup/...` (rclone speaks S3;
  point a remote at the endpoint) to a disk on the *other* Proxmox host.
- Snapshot `/var/lib/garage` at the VM/ZFS level alongside the Postgres
  VM backups (stop-less: Garage tolerates crash-consistent snapshots of
  LMDB + data dirs, but prefer the rclone path for restore simplicity).

Course content is also fully reproducible from `courses/*.md` in git —
the bucket is a render/serving tier, not the only copy. User data stays
in Postgres, which keeps its own backup story.

## 7. Ops crib sheet

```sh
garage status                 # node health, layout
garage stats                  # per-bucket object/byte counts
garage bucket info rubber-duck-courses
garage key list
garage block list-errors      # missing/corrupt blocks (after disk trouble)
journalctl -u garage -f
```
