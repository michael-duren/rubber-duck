# Phase 1 — Run the server on the existing homelab

Concrete, executable version of Phase 1 for the homelab **as actually
built** (see the `home-infra` repo). Supersedes the boots-on-the-ground
detail of [`local-migration-plan.md`](local-migration-plan.md) Phases 1–4,
which was written before the hardware existed and assumes VMs built by hand

- a Cloudflare Tunnel. Strategic intent (ADR 0001) is unchanged; this is
  just the "do it now" list against real IPs.

**Goal:** the Duck server running in the real k3s cluster, talking to a
real Postgres VM, image pulled from GHCR, reachable on the LAN. **Grading
is intentionally NOT wired** — it's a background auditor (`GC_GRADER`
unset → the server boots fine; only actual grade attempts error). The
`k8sgrader` is a later phase.

Nothing here touches prod (still on GCP). No data migration yet — this
brings up an empty DB the app migrates itself on boot.

## Ground truth (from `home-infra`)

| Host                             | IP             | Role                                              |
| -------------------------------- | -------------- | ------------------------------------------------- |
| pve-main (`touchgrass`)          | 192.168.20.2   | Proxmox node 1 — services + control plane         |
| pve-worker (`touchgrassworkers`) | 192.168.20.100 | Proxmox node 2 — workers                          |
| k8s-cp-1                         | 192.168.20.3   | k3s **server** (Traefik enabled, default install) |
| k8s-w-1 / k8s-w-2                | .101 / .102    | worker VMs (join status: verify below)            |

Provisioning pattern to reuse: `tf/` clones the `debian-template` module →
Ansible `baseline.yml` + role playbooks configure. VMs get static IPs via
cloud-init, user `ops`, your SSH key.

## Decisions locked for Phase 1

1. **DB is a terraform-managed Proxmox VM** (`duck-db`, 192.168.20.10, on
   the control node) — _yes, put it in terraform_: every other node is
   reproducible, the DB shouldn't be the one snowflake. Answers your open
   question. It supersedes the strategic plan's hand-built `db1`.
2. **Postgres 17** (matches prod Cloud SQL) via the PGDG repo, configured
   by a new `postgres.yml` playbook.
3. **`sslmode=disable`** on the trusted `.20.0/24` server LAN (strategic
   plan open-decision #2 — revisit if the subnet ever becomes shared).
4. **Images → GHCR**, public to start (no pull secret). First push by hand
   from your laptop; GHA workflow added at the end.
5. **Reach the app via `kubectl port-forward` first**, then an optional
   Traefik Ingress + `/etc/hosts`. No Cloudflare Tunnel this phase.
6. **1 replica.** Scale/spread comes once workers are confirmed joined.

## Networking gotcha (read before writing `pg_hba`)

k3s (flannel) **SNATs pod egress to the node IP**. From Postgres's view,
connections from app pods arrive as the _node_ address
(192.168.20.3/.101/.102), **not** the pod CIDR (10.42.0.0/16). So
`pg_hba.conf` must trust `192.168.20.0/24`, not the pod network. Scoping to
the pod CIDR would silently reject every connection.

---

## Step 0 — Prerequisites

```sh
# From wherever you run kubectl against the cluster:
export KUBECONFIG=/path/to/kubeconfig-homelab
kubectl get nodes -o wide
```

- Confirm `k8s-cp-1` is `Ready`. If `k8s-w-1/2` are absent, the workers
  aren't joined yet — **fine for Phase 1** (the server node runs workloads;
  k3s doesn't taint it). Joining them is a later step, not a blocker.
- Confirm Traefik is up: `kubectl -n kube-system get pods | grep traefik`.

---

## Step 1 — DB VM via terraform (`home-infra`)

Create `tf/db.tf`. It clones the **control-node** template (the DB lives on
pve-main, the "services" node) exactly like `k8s-cp-1` does:

```hcl
# tf/db.tf
# Postgres VM for the Duck app. Same clone-the-template pattern as the k8s
# nodes so the DB host is reproducible, not hand-built.
resource "proxmox_virtual_environment_vm" "duck_db" {
  name      = "duck-db"
  node_name = var.control_node_name
  vm_id     = 205

  clone {
    vm_id = module.debian_template_control.vm_id
    full  = true
  }

  cpu {
    cores = 2
    type  = "host"
  }

  memory {
    dedicated = 4096
  }

  agent {
    enabled = true
  }

  initialization {
    ip_config {
      ipv4 {
        address = "192.168.20.10/24"
        gateway = "192.168.20.1"
      }
    }

    user_account {
      username = "ops"
      keys     = [trimspace(file("~/.ssh/id_ed25519.pub"))]
    }
  }
}
```

Apply:

```sh
cd tf
tofu plan     # expect: 1 VM to add, nothing else changed
tofu apply
```

Verify: `ssh ops@192.168.20.10` works. (Template disk is 20 GB — plenty for
the metadata DB; grow the disk later if needed.)

---

## Step 2 — Postgres via ansible (`home-infra`)

**2a. Add the DB to the inventory** (`ansible/inventory.ini`):

```ini
[db]
duck-db ansible_host=192.168.20.10

[db:vars]
ansible_user=ops
```

**2b. Write `ansible/postgres.yml`.** Installs the guest agent + Postgres
17 from PGDG, opens it to the LAN, and creates the role/db. The password
comes in as an extra-var (don't commit it):

```yaml
- name: Postgres 17 for the Duck app
  hosts: db
  become: true
  vars:
    duck_db_name: duckserver
    duck_db_user: duckserver
    # pass at runtime: --extra-vars "duck_db_password=..."  (or vault it)
  tasks:
    - name: Base packages + guest agent
      ansible.builtin.apt:
        name:
          [
            qemu-guest-agent,
            curl,
            vim,
            ca-certificates,
            gnupg,
            python3-psycopg2,
          ]
        update_cache: true

    - name: Enable guest agent
      ansible.builtin.systemd_service:
        name: qemu-guest-agent
        state: started
        enabled: true

    - name: Add PGDG apt key
      ansible.builtin.get_url:
        url: https://www.postgresql.org/media/keys/ACCC4CF8.asc
        dest: /usr/share/keyrings/pgdg.asc
        mode: "0644"

    - name: Add PGDG repo
      ansible.builtin.apt_repository:
        repo: "deb [signed-by=/usr/share/keyrings/pgdg.asc] http://apt.postgresql.org/pub/repos/apt bookworm-pgdg main"
        filename: pgdg

    - name: Install Postgres 17
      ansible.builtin.apt:
        name: postgresql-17
        update_cache: true

    - name: Listen on all interfaces
      ansible.builtin.lineinfile:
        path: /etc/postgresql/17/main/postgresql.conf
        regexp: "^#?listen_addresses"
        line: "listen_addresses = '*'"
      notify: restart postgres

    # SNAT means app pods arrive as the node IP — trust the server subnet,
    # NOT the pod CIDR. scram-sha-256 is PG17's default.
    - name: Allow the cluster subnet
      ansible.builtin.lineinfile:
        path: /etc/postgresql/17/main/pg_hba.conf
        line: "host  {{ duck_db_name }}  {{ duck_db_user }}  192.168.20.0/24  scram-sha-256"
      notify: restart postgres

    - name: Create role
      become_user: postgres
      community.postgresql.postgresql_user:
        name: "{{ duck_db_user }}"
        password: "{{ duck_db_password }}"

    - name: Create database
      become_user: postgres
      community.postgresql.postgresql_db:
        name: "{{ duck_db_name }}"
        owner: "{{ duck_db_user }}"

  handlers:
    - name: restart postgres
      ansible.builtin.systemd_service:
        name: postgresql
        state: restarted
```

If `community.postgresql` isn't installed:
`ansible-galaxy collection install community.postgresql`.

**2c. Run it** (pick a strong password; you'll reuse it in Step 4):

```sh
cd ansible
ansible-playbook -i inventory.ini postgres.yml \
  --extra-vars "duck_db_password=CHANGE_ME_STRONG"
```

**Verify from your workstation** (proves the LAN + pg_hba path the pods
will use):

```sh
psql "postgres://duckserver:CHANGE_ME_STRONG@192.168.20.10:5432/duckserver?sslmode=disable" -c '\conninfo'
```

Migrations run automatically when the app first boots (`main.go` calls
`store.Migrate` on serve) — nothing to run by hand.

---

## Step 3 — Build & push the image to GHCR

The `Dockerfile` already builds a static binary and `EXPOSE 8080`; no
changes needed. First push by hand so you're not debugging CI and k8s at
once:

```sh
# from the app repo root
echo "$GHCR_PAT" | docker login ghcr.io -u YOUR_GH_USERNAME --password-stdin
docker build -t ghcr.io/YOUR_GH_USERNAME/duck:$(git rev-parse --short HEAD) .
docker push ghcr.io/YOUR_GH_USERNAME/duck:$(git rev-parse --short HEAD)
```

Then in GitHub → the package's settings, set visibility **Public** (skips
the pull secret entirely). Note the pushed tag for Step 4.

---

## Step 4 — Deploy to k3s (app repo, `deploy/`)

Matches the strategic plan's `deploy/` convention. Create these three
files, then the two secrets out-of-band.

**Secrets (not committed):**

```sh
kubectl create namespace duck

kubectl -n duck create secret generic duck-db \
  --from-literal=DATABASE_URL='postgres://duckserver:CHANGE_ME_STRONG@192.168.20.10:5432/duckserver?sslmode=disable'
```

**`deploy/deployment.yaml`:**

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: duck
  namespace: duck
spec:
  replicas: 1
  selector:
    matchLabels: { app: duck }
  template:
    metadata:
      labels: { app: duck }
    spec:
      containers:
        - name: duck
          image: ghcr.io/YOUR_GH_USERNAME/duck:REPLACE_WITH_SHA
          ports:
            - containerPort: 8080
          envFrom:
            - secretRef: { name: duck-db } # injects DATABASE_URL
          # GC_GRADER unset → boots fine; grade attempts error (auditor only).
          readinessProbe:
            httpGet: { path: /, port: 8080 }
            initialDelaySeconds: 5
          resources:
            requests: { cpu: 100m, memory: 128Mi }
            limits: { cpu: "1", memory: 512Mi }
```

**`deploy/service.yaml`:**

```yaml
apiVersion: v1
kind: Service
metadata:
  name: duck
  namespace: duck
spec:
  selector: { app: duck }
  ports:
    - port: 80
      targetPort: 8080
```

Apply and watch it come up:

```sh
kubectl apply -f deploy/deployment.yaml -f deploy/service.yaml
kubectl -n duck rollout status deploy/duck
kubectl -n duck logs deploy/duck   # expect: migrations ran, "listening addr=:8080"
```

**Reach it (port-forward first):**

```sh
kubectl -n duck port-forward svc/duck 8080:80
# browse http://localhost:8080
```

---

## Step 5 — Optional: Traefik Ingress on the LAN

Once port-forward works, expose it on a hostname. k3s ships Traefik +
servicelb, so no add-ons.

**`deploy/ingress.yaml`:**

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: duck
  namespace: duck
spec:
  rules:
    - host: duck.home.lan
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: duck
                port: { number: 80 }
```

```sh
kubectl apply -f deploy/ingress.yaml
```

Point the hostname at any node IP (Traefik binds all of them). Simplest —
add to your workstation's `/etc/hosts`:

```
192.168.20.3  duck.home.lan
```

Then browse `http://duck.home.lan`. (`CanonicalHost` only redirects `www.`
hosts, so a bare LAN name passes straight through — no app change needed.)

---

## Step 6 — Wire CI (GHA → GHCR)

Leave the GCP `cd.yml` alone; add `.github/workflows/homelab-image.yml`:

```yaml
name: homelab-image
on:
  push:
    branches: [main]
permissions:
  contents: read
  packages: write
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - uses: docker/build-push-action@v6
        with:
          context: .
          push: true
          tags: |
            ghcr.io/YOUR_GH_USERNAME/duck:${{ github.sha }}
            ghcr.io/YOUR_GH_USERNAME/duck:latest
```

This only _builds and pushes_ — it does not deploy. Auto-sync to the
cluster is Phase 2 (ArgoCD watching `deploy/`). For now, redeploy by
bumping the image tag and re-applying.

---

## Done when

- [ ] `tofu apply` created `duck-db`; `ssh ops@192.168.20.10` works.
- [ ] `postgres.yml` ran; `psql` from your workstation connects.
- [ ] Image is in GHCR (public) at a known tag.
- [ ] `kubectl -n duck logs deploy/duck` shows migrations + `listening`.
- [ ] App loads over port-forward (and optionally `duck.home.lan`).
- [ ] GHA workflow pushes an image on merge to `main`.

## Explicitly deferred

- **Grading** (`k8sgrader`) — background auditor; server is fully usable
  without it. Strategic plan Phase 5.
- **ArgoCD / GitOps auto-sync** — Phase 2. Runs _in_ the cluster, not a
  separate VM.
- **Prod data migration + DNS cutover** — strategic plan Phases 7.
- **Backups (R2), monitoring, gVisor, worker join/spread** — later phases.
