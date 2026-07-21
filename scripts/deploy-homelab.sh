#!/usr/bin/env bash
# Deploy the duckserver POC to the homelab k3s cluster.
#
# There is no image registry: the app image and the gc-runner-* images are
# docker-saved and imported into every node's containerd (k3s ctr), so both
# the app pod and the per-submission grading pods (GC_GRADER=k8s) can
# schedule anywhere. The only docker daemon involved is the local one doing
# the builds — the cluster nodes run containerd only.
#
# Requires:
#   - docker locally, kubectl + the homelab kubeconfig
#   - ssh access as ops@ the nodes (same key ansible uses)
#   - DUCK_DB_PASSWORD in the environment (the duckserver role password on
#     pgdb, see home-infra/infra/ansible/.env)
set -euo pipefail

cd "$(dirname "$0")/.."

KUBECONFIG="${KUBECONFIG:-$HOME/Code/home-infra/infra/ansible/kubeconfig-homelab}"
export KUBECONFIG

DB_HOST="${DUCK_DB_HOST:-192.168.20.103}"
NODES=(192.168.20.3 192.168.20.101 192.168.20.102)

: "${DUCK_DB_PASSWORD:?set DUCK_DB_PASSWORD (duckserver role password on pgdb)}"

echo "==> building app image"
docker build -t duckserver:homelab .

echo "==> building runner images"
make runner-images

for ip in "${NODES[@]}"; do
  echo "==> $ip: importing images into containerd"
  docker save duckserver:homelab gc-runner-go gc-runner-python gc-runner-c |
    ssh "ops@$ip" sudo k3s ctr images import -
done

echo "==> applying manifests"
kubectl apply -f deploy/homelab/namespace.yaml
kubectl -n duck create secret generic duckserver-db \
  --from-literal=DATABASE_URL="postgres://duckserver:${DUCK_DB_PASSWORD}@${DB_HOST}:5432/duckserver?sslmode=disable" \
  --dry-run=client -o yaml | kubectl apply -f -
# cloudflared tunnel token: only needed on first deploy / rebuild. Grab it
# from Zero Trust -> Networks -> Tunnels -> duck-homelab -> install command.
if [ -n "${TUNNEL_TOKEN:-}" ]; then
  kubectl -n duck create secret generic cloudflared-token \
    --from-literal=TUNNEL_TOKEN="${TUNNEL_TOKEN}" \
    --dry-run=client -o yaml | kubectl apply -f -
elif ! kubectl -n duck get secret cloudflared-token >/dev/null 2>&1; then
  echo "error: cloudflared-token secret missing and TUNNEL_TOKEN not set" >&2
  exit 1
fi
kubectl apply -f deploy/homelab/

echo "==> waiting for rollout"
kubectl -n duck rollout restart deployment/duckserver >/dev/null 2>&1 || true
kubectl -n duck rollout status deployment/duckserver --timeout=180s

echo
echo "duckserver is up. Add to /etc/hosts if you haven't:"
echo "  192.168.20.3  duck.homelab"
echo "then open http://duck.homelab"
