# Homelab POC deployment

Runs duckserver on the home k3s cluster against the `pgdb` Proxmox VM, with
grading as per-submission Kubernetes pods (`GC_GRADER=k8s`). Nothing here
touches GCP, and the cluster nodes run containerd only — no docker daemon.

```
workstation                 homelab (192.168.20.0/24)
  make deploy-homelab  -->  k3s: duck namespace, 1 duckserver pod
                              pod -> pgdb VM (192.168.20.103, Postgres 17)
                              pod -> kube API -> gc-runner-* grading pods
```

## How the pieces land

- **No registry.** `scripts/deploy-homelab.sh` docker-saves the app image and
  the `gc-runner-*` images into every node's containerd
  (`k3s ctr images import`); all pods use `imagePullPolicy: Never`. The only
  docker daemon involved is the workstation's, for building.
- **Grading.** `internal/grader/k8sgrader` creates one pod per submission:
  the solution + tests ride a ConfigMap mounted at `/job`, and the pod
  command tars them into `/run.sh`'s stdin — the exact local-docker runner
  contract, so runner images are unchanged. Exit code = result, pod log =
  output. `rbac.yaml` scopes the app's service account to exactly
  pods/configmaps create-get-delete, and `networkpolicy.yaml` gives grading
  pods no network at all (k3s enforces NetworkPolicy).
- **Database.** The playbook home-infra/infra/ansible/postgres.yml provisions
  the `duckserver` role/db on the pgdb VM and opens pg_hba to the server
  subnet (pod traffic arrives SNATed as the node IP). The password becomes
  the `duckserver-db` k8s secret at deploy time — it is never committed.
- **Migrations** run automatically on pod start (`serve` calls
  `store.Migrate`).
- **Ingress** is k3s' bundled traefik, host `duck.homelab`. Point it at any
  node, e.g. `/etc/hosts`: `192.168.20.3 duck.homelab`.

## Deploy

```sh
export DUCK_DB_PASSWORD=...   # from home-infra/infra/ansible/.env
make deploy-homelab
```

## Seed courses

Same direct-to-db seeding as `make seed`, but pointed at pgdb:

```sh
DB="postgres://duckserver:${DUCK_DB_PASSWORD}@192.168.20.103:5432/duckserver?sslmode=disable"
for f in seed/intro-to-go.md courses/*.md; do
  go run ./cmd/duckserver seed --db "$DB" "$f"
done
```

## POC limits (deliberate)

- Single replica; image distribution is per-node by hand, not a registry.
- Grading pods have k8s resource caps and no network, but share the node
  kernel — same isolation class as the M1 docker grader, one step below
  prod's gVisor.
- No TLS; sessions ride plain http inside the LAN.
