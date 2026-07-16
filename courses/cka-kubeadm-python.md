---
course: cka-kubeadm
title: The CKA Path — Kubernetes From Parts
language: python
description: >
  Build vanilla Kubernetes the way the CKA exam expects you to run it:
  prep nodes and the container runtime by hand, bootstrap a control plane
  with kubeadm and dissect what it made, join workers, then go
  highly-available with three control planes behind a load balancer. From
  that cluster, drill every exam domain — workloads and scheduling,
  services and network policy, storage, RBAC, upgrades and etcd backups,
  Helm and Kustomize — ending with the troubleshooting practice that is
  30% of the exam and a timed mock. Checkpoints are self-attested: you do
  the work on your own VMs and claim the points.
duration_hours: 24
tags: [kubernetes, infrastructure, devops, certification]
extended_reading:
  - title: Kubernetes documentation (allowed during the exam)
    url: https://kubernetes.io/docs/
  - title: CKA curriculum (the official domain list)
    url: https://github.com/cncf/curriculum
  - title: kubeadm reference
    url: https://kubernetes.io/docs/reference/setup-tools/kubeadm/
  - title: killer.sh — the CKA simulator included with exam registration
    url: https://killer.sh
---

# Lesson: The Exam and the Lab {#exam-and-lab}

The Certified Kubernetes Administrator exam is **performance-based**: two
hours, roughly 15–20 hands-on tasks executed over SSH against real
clusters, 66% to pass. No multiple choice — you either make the cluster
do the thing or you don't. Its published domains, and roughly how this
course weights its time:

- **Troubleshooting — 30%.** The biggest domain, and the whole back half
  of this course feeds it.
- **Cluster Architecture, Installation & Configuration — 25%.** kubeadm,
  HA, RBAC, the lifecycle lessons.
- **Services & Networking — 20%**, **Workloads & Scheduling — 15%**,
  **Storage — 10%.**

Two facts should shape how you study. First, **the Kubernetes docs are
allowed during the exam** — so the skill isn't memorizing YAML, it's
knowing which docs page has the copy-pasteable example and how to adapt
it fast. Practice with the docs open, always. Second, the exam terminal
is plain Linux over SSH: your speed tools are `kubectl` muscle memory,
not your dotfiles. Adopt the habits now:

```bash
alias k=kubectl                     # provided in the real exam, too
export do="--dry-run=client -o yaml"   # k create deploy web --image=nginx $do
kubectl completion bash             # source it; tab-complete everything
kubectl explain deployment.spec.strategy   # docs without leaving the terminal
```

**A note on how this course grades.** Your work happens on real VMs, not
in a code runner. Every checkpoint is *self-attested*: do the work, flip
`DONE = False` to `True`, submit. Nothing validates you — the exam
eventually will.

## The lab

You need six VMs on one network. Where they come from doesn't matter —
Proxmox, VirtualBox, a cloud free tier, or (fittingly) the Terraform
setup from the Proxmox homelab course on this platform. Ubuntu 24.04 is
the sensible guest OS: it's what the exam environment runs. 2 vCPU / 4 GB
each; the load balancer can be tiny (1 vCPU / 1 GB).

| Host      | IP             | Role                                  |
|-----------|----------------|---------------------------------------|
| `k8s-lb`  | `192.168.1.50` | API-server load balancer (haproxy)    |
| `cp-1`    | `192.168.1.51` | Control plane 1 (first to init)       |
| `cp-2`    | `192.168.1.52` | Control plane 2 (joins in the HA lesson) |
| `cp-3`    | `192.168.1.53` | Control plane 3 (joins in the HA lesson) |
| `w-1`     | `192.168.1.55` | Worker                                |
| `w-2`     | `192.168.1.56` | Worker                                |

Substitute your own subnet throughout. One address decision matters *now*
even though HA comes later: the cluster will know its API server by the
**name** `k8s-api`, never by an IP, so the endpoint can move onto the
load balancer without re-bootstrapping. Put this line in `/etc/hosts` on
**every VM and your workstation** — pointing at `cp-1` for the time
being:

```text
192.168.1.51 k8s-api
```

Also pick your **pod network CIDR** now: `10.244.0.0/16`. Pod IPs must
not overlap the LAN the nodes live on (`192.168.1.0/24` here) — overlap
produces maddening "some pods can't reach some nodes" bugs, and the exam
expects you to know the flag that sets it.

## Challenge: Six Machines and a Plan {#exam-and-lab-checkpoint points=5}

All six VMs up, SSH-reachable, unique hostnames matching the table, and
`k8s-api → cp-1` resolving from every machine.

### Starter

```python
# Self-attested checkpoint — flip DONE once you've honestly done the work.

# Six Ubuntu VMs are up with the planned hostnames/IPs, I can SSH to all
# of them, and `ping k8s-api` resolves to cp-1 from every node and my
# workstation.
DONE = False

# For your own record: where the VMs live, your actual IP plan.
NOTES = """
"""
```

### Tests

```python
from solution import DONE


def test_checkpoint_claimed():
    assert DONE is True, "flip DONE to True once the lab is reachable"
```

# Lesson: Node Prep — Runtime, Kubelet, and the Three Gotchas {#node-prep}

Every cluster node — control plane or worker — needs identical plumbing
before kubeadm will touch it: a container runtime, the Kubernetes
packages, and three OS settings that each have a classic failure mode
attached. Run this whole lesson on **all five cluster VMs** (`cp-1..3`,
`w-1..2`; the load balancer is exempt — it never runs pods). Doing it
five times by hand once is instructive; scripting it (or writing the
Ansible playbook) afterwards is encouraged.

## Gotcha 1: swap

The kubelet refuses to start with swap enabled — Kubernetes manages
memory per-pod and swap makes its accounting lie. Off, and off after
reboot:

```bash
sudo swapoff -a
sudo sed -i '/ swap / s/^/#/' /etc/fstab
```

## Gotcha 2: kernel modules and sysctls

Pod traffic crosses Linux bridges and gets forwarded between interfaces;
neither is subjected to iptables (where kube-proxy programs service
routing) nor allowed at all by default:

```bash
cat <<EOF | sudo tee /etc/modules-load.d/k8s.conf
overlay
br_netfilter
EOF
sudo modprobe overlay && sudo modprobe br_netfilter

cat <<EOF | sudo tee /etc/sysctl.d/k8s.conf
net.bridge.bridge-nf-call-iptables  = 1
net.bridge.bridge-nf-call-ip6tables = 1
net.ipv4.ip_forward                 = 1
EOF
sudo sysctl --system
```

## Gotcha 3: the runtime's cgroup driver

The runtime is **containerd** — kubelet starts containers by talking to
it over the CRI socket. Ubuntu packages it, but with a config that
mismatches the kubelet's expectations in the single most infamous way in
Kubernetes setup: both kubelet and runtime must use the **same cgroup
driver**, and on a systemd distro that driver must be `systemd`, while
containerd's default config says `SystemdCgroup = false`. The symptom of
skipping this is a cluster that inits fine and then collapses minutes
later as pods churn. Fix it before it happens:

```bash
sudo apt install -y containerd
sudo mkdir -p /etc/containerd
containerd config default | sudo tee /etc/containerd/config.toml >/dev/null
sudo sed -i 's/SystemdCgroup = false/SystemdCgroup = true/' /etc/containerd/config.toml
sudo systemctl restart containerd
```

## The Kubernetes packages

`kubeadm` (the bootstrapper), `kubelet` (the node agent), and `kubectl`
come from the project's own apt repo, which is **pinned per minor
version** — the URL contains `v1.34`, and that repo only ever serves
1.34.x. Deliberately install **one minor behind the newest release**: the
upgrade lesson needs somewhere real to upgrade to. (Check the current
latest at kubernetes.io/releases and subtract one; commands below assume
1.34 with 1.35 current — substitute.)

```bash
sudo apt install -y apt-transport-https ca-certificates curl gpg
sudo mkdir -p /etc/apt/keyrings
curl -fsSL https://pkgs.k8s.io/core:/stable:/v1.34/deb/Release.key \
  | sudo gpg --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg
echo 'deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/v1.34/deb/ /' \
  | sudo tee /etc/apt/sources.list.d/kubernetes.list
sudo apt update && sudo apt install -y kubelet kubeadm kubectl
sudo apt-mark hold kubelet kubeadm kubectl
```

`apt-mark hold` matters: cluster version changes must happen through the
deliberate upgrade procedure, never as a side effect of `apt upgrade`.
The kubelet will now crash-loop quietly until kubeadm gives it a cluster
to belong to — that's expected; leave it.

## Challenge: Five Nodes Ready for Duty {#node-prep-checkpoint points=15}

All five cluster VMs prepped: swap off (and off in fstab), modules and
sysctls set, containerd running with the systemd cgroup driver, and
held kubelet/kubeadm/kubectl packages one minor behind current.

### Starter

```python
# Self-attested checkpoint — flip DONE once you've honestly done the work.

# On cp-1..3 and w-1..2: swapoff persistent, br_netfilter/ip_forward
# sysctls applied, SystemdCgroup=true in containerd's config, and
# kubeadm/kubelet/kubectl installed and held at one minor behind latest.
DONE = False

# For your own record: the exact version you pinned; whether you scripted
# the prep or ran it by hand.
NOTES = """
"""
```

### Tests

```python
from solution import DONE


def test_checkpoint_claimed():
    assert DONE is True, "flip DONE to True once all five nodes are prepped"
```

# Lesson: kubeadm init — Anatomy of a Control Plane {#control-plane-anatomy}

`kubeadm init` bootstraps a control plane on one machine. Where k3s hides
the components inside a binary, kubeadm lays them out where you can
touch them — which is exactly why this path teaches (and examines)
better. On `cp-1`:

```bash
sudo kubeadm init \
  --control-plane-endpoint "k8s-api:6443" \
  --pod-network-cidr 10.244.0.0/16 \
  --upload-certs
```

Both flags encode decisions from the lab lesson: the **name** the cluster
knows its API by (so HA can move it later — omitting this at init is the
one mistake you can't cheaply undo), and the pod CIDR the network plugin
will carve pod IPs from. Watch the output scroll: generating certs,
writing kubeconfigs, starting the control plane, uploading certs, then a
success message containing **two join commands — save both** (one for
control planes, one for workers).

Give yourself credentials — the admin kubeconfig kubeadm wrote is
root-on-cluster:

```bash
mkdir -p ~/.kube && sudo cp /etc/kubernetes/admin.conf ~/.kube/config
sudo chown $(id -u):$(id -g) ~/.kube/config
kubectl get nodes    # one node... NotReady. Correct! See below.
```

## The tour — learn this layout cold

The exam's troubleshooting tasks are largely "something in this layout is
wrong, find it." Spend real time here:

The kubelet is the bootstrap: it watches `/etc/kubernetes/manifests/` and
runs the four control-plane components as **static pods** — which is why
editing (or breaking) a file there makes that component restart (or vanish).

```d2
kubelet: "kubelet\n(watches manifests/)" {style.stroke: "#fbbf24"; style.stroke-width: 2}
pods: "static pods · kube-system" {
  api: "kube-apiserver"
  etcd: "etcd\n(all cluster state)"
  sched: "scheduler"
  ctrl: "controller-manager"
}
kubelet -> pods.api: runs
kubelet -> pods.etcd: runs
kubelet -> pods.sched: runs
kubelet -> pods.ctrl: runs
```

- **`/etc/kubernetes/manifests/`** — four YAML files: `etcd`,
  `kube-apiserver`, `kube-controller-manager`, `kube-scheduler`. These
  are **static pods**: the kubelet watches this directory and runs
  whatever it finds, no API server required — which resolves the
  chicken-and-egg of who starts the API server. Edit a file here and the
  kubelet restarts that component; break one (typo an image name and
  watch) and that component is simply gone. `kubectl -n kube-system get
  pods` shows them with the node name suffixed, e.g. `etcd-cp-1`.
- **`etcd`** — the cluster's entire state in one key-value store. Every
  object you ever `apply` lives here; everything else is stateless and
  rebuildable. This is why the backup lesson is really "the etcd lesson."
- **`/etc/kubernetes/pki/`** — the certificate authority and every cert
  the components use to trust each other. `kubeadm certs
  check-expiration` reads this; expired certs here are a classic broken
  cluster.
- **`/etc/kubernetes/*.conf`** — kubeconfigs for kubelet, controller
  manager, scheduler, admin: identity is client certificates, all the way
  down.

And the answer to `NotReady`: `kubectl describe node cp-1` says the
network plugin isn't ready. Kubernetes ships **no pod network** — the CNI
is your choice and the next lesson's job. Notice CoreDNS pods sitting
`Pending` for the same reason. Being able to *read* this state calmly is
worth more than memorizing any fix.

## Challenge: One Brain, Dissected {#control-plane-checkpoint points=15}

Control plane initialized with the endpoint name and pod CIDR; both join
commands saved; and you can navigate the manifests/pki/kubeconfig layout
without a map — prove it to yourself by breaking and unbreaking a static
pod.

### Starter

```python
# Self-attested checkpoint — flip DONE once you've honestly done the work.

# kubeadm init succeeded with --control-plane-endpoint k8s-api:6443 and
# the pod CIDR; kubectl works on cp-1; I broke the scheduler's static pod
# manifest, watched it vanish from kube-system, and restored it.
DONE = False

# For your own record: which file you broke and what the failure looked like.
NOTES = """
"""
```

### Tests

```python
from solution import DONE


def test_checkpoint_claimed():
    assert DONE is True, "flip DONE to True once the anatomy tour is done"
```

# Lesson: CNI and Joining Workers {#cni-and-workers}

Pods need IPs, and pods on different nodes need routes to each other.
Kubernetes defines the interface for this (the Container Network
Interface) and implements none of it. We install **Calico**: it's
widely deployed, and — decisive for the exam — it *enforces
NetworkPolicy*, which the simplest plugins silently don't.

Two things happen in this lesson: workers register with the control plane
via `kubeadm join` (dashed), and Calico lays a pod network
(`10.244.0.0/16`) over every node so pods on different machines can route
to each other.

```d2
direction: right
cp: "cp-1\ncontrol plane" {style.stroke: "#a78bfa"; style.stroke-width: 2}
w1: "w-1\nkubelet + pods"
w2: "w-2\nkubelet + pods"
w1 -> cp: "kubeadm join" {style.stroke-dash: 4}
w2 -> cp: "kubeadm join" {style.stroke-dash: 4}
```

## Install Calico

Calico installs as an operator plus a config object. Check the [Calico
docs](https://docs.tigera.io/calico/latest/getting-started/kubernetes/)
for the current version and substitute it below:

```bash
kubectl create -f https://raw.githubusercontent.com/projectcalico/calico/v3.30.0/manifests/tigera-operator.yaml
curl -LO https://raw.githubusercontent.com/projectcalico/calico/v3.30.0/manifests/custom-resources.yaml
```

**Edit `custom-resources.yaml` before applying**: its default pod CIDR is
`192.168.0.0/16`, which collides with our LAN and doesn't match what we
told kubeadm. Change the `cidr:` field to `10.244.0.0/16`, then:

```bash
kubectl create -f custom-resources.yaml
watch kubectl get pods -A
```

Over a few minutes: calico pods roll out, CoreDNS leaves `Pending`, and
`kubectl get nodes` flips to `Ready`. That sequence — CNI up, DNS
schedulable, node Ready — is a causal chain worth remembering in reverse:
a `Pending` CoreDNS on a fresh cluster means CNI trouble.

## Join the workers

On `w-1` and `w-2`, run the worker join command saved from init — it
looks like:

```bash
sudo kubeadm join k8s-api:6443 --token <token> \
  --discovery-token-ca-cert-hash sha256:<hash>
```

The token proves the node's allowed in; the CA hash proves to the *node*
it's talking to the right cluster — mutual suspicion, resolved. Tokens
expire after 24h; regenerate the whole command anytime with
`kubeadm token create --print-join-command` (memorize that one — the
exam doesn't hand you saved output).

```bash
kubectl get nodes -o wide     # cp-1, w-1, w-2 — all Ready
```

Note what kubeadm did that k3s also did but silently: the control plane
carries the taint `node-role.kubernetes.io/control-plane:NoSchedule` out
of the box — workloads land on workers only. Verify with `kubectl
describe node cp-1 | grep -i taint`. Deploy something disposable to
prove the data path:

```bash
kubectl create deployment hello --image=nginx --replicas=2
kubectl get pods -o wide      # spread across w-1/w-2, IPs from 10.244.x.x
kubectl delete deployment hello
```

## Challenge: A Real Cluster {#cni-workers-checkpoint points=15}

Calico installed with the corrected CIDR, both workers joined (at least
one via a *freshly generated* join command), all nodes Ready, and a test
deployment scheduled onto the workers with pod-CIDR IPs.

### Starter

```python
# Self-attested checkpoint — flip DONE once you've honestly done the work.

# Calico is up with cidr 10.244.0.0/16, w-1 and w-2 joined (I regenerated
# a join command with kubeadm token create), all three nodes are Ready,
# and a scratch nginx deployment landed on the workers.
DONE = False

# For your own record: how long CNI rollout took; the join command flags.
NOTES = """
"""
```

### Tests

```python
from solution import DONE


def test_checkpoint_claimed():
    assert DONE is True, "flip DONE to True once nodes are Ready and pods run"
```

# Lesson: Going HA — Three Control Planes Behind a Load Balancer {#ha-control-plane}

One control plane is a homelab; three is how Kubernetes runs anywhere
that matters, and operating an HA cluster is explicitly in the exam's
biggest non-troubleshooting domain. The architecture we're building is
**stacked etcd**: each control-plane node runs its own etcd member
alongside the other components, the three etcd members form a quorum
(majority rules — three members tolerate one failure), and a TCP load
balancer in front makes the API reachable when any one node is down.

```d2
direction: right
lb: "haproxy\nk8s-api:6443\n(TCP LB)" {shape: hexagon; style.stroke: "#fbbf24"; style.stroke-width: 2}
quorum: "stacked etcd · quorum 2 of 3" {
  cp1: "cp-1\napiserver + etcd"
  cp2: "cp-2\napiserver + etcd"
  cp3: "cp-3\napiserver + etcd"
}
lb -> quorum.cp1
lb -> quorum.cp2
lb -> quorum.cp3
```

## The load balancer

On `k8s-lb`, haproxy in plain TCP mode — no TLS termination, it just
moves bytes to whichever API server is healthy:

```bash
sudo apt install -y haproxy
```

Append to `/etc/haproxy/haproxy.cfg`:

```text
frontend kubernetes-api
    bind *:6443
    mode tcp
    option tcplog
    default_backend control-planes

backend control-planes
    mode tcp
    option tcp-check
    balance roundrobin
    server cp-1 192.168.1.51:6443 check
    server cp-2 192.168.1.52:6443 check
    server cp-3 192.168.1.53:6443 check
```

```bash
sudo systemctl restart haproxy
```

Two of the three backends don't exist yet; `check` marks them down and
haproxy carries on with `cp-1`. Now collect the payoff of that
init-lesson foresight: the cluster knows its API as `k8s-api`, so moving
the endpoint is a hosts-file edit. On **every node and your
workstation**, repoint:

```text
192.168.1.50 k8s-api
```

`kubectl get nodes` still works — traffic now flows through the LB.

## Join two more control planes

A control-plane join needs the worker-join credentials *plus* the
cluster's certificate bundle. kubeadm shares certs through a
short-lived (2-hour) encrypted secret; re-upload and get the decryption
key, then a fresh join command, both on `cp-1`:

```bash
sudo kubeadm init phase upload-certs --upload-certs   # prints <cert-key>
kubeadm token create --print-join-command             # prints the base command
```

On `cp-2` and `cp-3`, combine them:

```bash
sudo kubeadm join k8s-api:6443 --token <token> \
  --discovery-token-ca-cert-hash sha256:<hash> \
  --control-plane --certificate-key <cert-key>
```

Then from anywhere: `kubectl get nodes` — five nodes, three of them
control planes. `kubectl -n kube-system get pods | grep etcd` shows three
etcd members.

## Prove the availability is real

Shut down `cp-1` entirely (`sudo poweroff`). Then, from your
workstation:

```bash
kubectl get nodes        # still answers — via cp-2/cp-3 through the LB
kubectl create deployment ha-proof --image=nginx
kubectl get pods         # scheduling still works: etcd quorum of 2/3 holds
```

The original control plane is *off* and the cluster takes writes. Boot
it back, watch it return to `Ready`, and clean up `ha-proof`. Also note
the honest limit: lose *two* control planes and etcd loses quorum — the
API goes read-only-at-best with one survivor. Quorum math, not magic.

## Challenge: Kill the First Brain {#ha-checkpoint points=15}

Three control planes behind haproxy, endpoint repointed to the LB
everywhere, and the powered-off-cp-1 test passed — cluster writable
throughout.

### Starter

```python
# Self-attested checkpoint — flip DONE once you've honestly done the work.

# cp-2 and cp-3 joined with --control-plane and the certificate key,
# k8s-api resolves to the LB on every machine, and with cp-1 powered off
# I created a deployment successfully before booting it back.
DONE = False

# For your own record: etcd member list output; how long cp-1 took to
# rejoin cleanly.
NOTES = """
"""
```

### Tests

```python
from solution import DONE


def test_checkpoint_claimed():
    assert DONE is True, "flip DONE to True once the cluster survived cp-1"
```

# Lesson: Workloads and Scheduling {#workloads-scheduling}

From here on you drill *operating* the cluster — and this lesson's meta-
skill is speed: the exam rewards generating YAML over writing it.
Everything starts from an imperative command plus `--dry-run`:

```bash
kubectl create deployment web --image=nginx:1.27 --replicas=3 \
  --dry-run=client -o yaml > web.yaml
kubectl apply -f web.yaml
```

Edit the file when a task needs what flags can't express; `kubectl
explain deployment.spec.template.spec --recursive | less` finds field
names faster than the docs. Now the exam's workload staples, each of
which you should *do*, not read:

**Rollouts.** Change the image and watch the Deployment replace pods in
a controlled wave; then take it back:

```bash
kubectl set image deployment/web nginx=nginx:1.28
kubectl rollout status deployment/web
kubectl rollout history deployment/web
kubectl rollout undo deployment/web
```

**Resources and probes.** Add to the container spec: `resources.requests`
(what the scheduler *reserves* — this is scheduling input) vs
`resources.limits` (where the runtime caps/kills — this is enforcement),
and a `readinessProbe`/`livenessProbe` pair. Deliberately set a wrong
probe port and watch the pod cycle — you'll debug this exact mistake in
the troubleshooting lesson.

**Config and secrets.** Create a ConfigMap and a Secret; consume one as
env vars and the other as a mounted volume — both directions are exam
fodder:

```bash
kubectl create configmap app-cfg --from-literal=MODE=prod
kubectl create secret generic app-secret --from-literal=TOKEN=hunter2
```

**Placement.** Three mechanisms, commonly confused:

- `nodeSelector` / node affinity — the *pod chooses* nodes ("run me on
  ssd nodes"). Label a worker (`kubectl label node w-1 disk=ssd`) and
  pin a pod to it.
- Taints + tolerations — the *node repels* pods except those that opt
  in. You've met this on control planes; taint `w-2` yourself, watch
  pods avoid it, add a toleration, untaint.
- **DaemonSets** — one pod per (eligible) node, for agents; Calico runs
  as one. Create a trivial one and note it lands on every worker.

**Static pods.** You know these from the control plane's anatomy; the
exam likes making *you* create one: drop a pod manifest into
`/etc/kubernetes/manifests/` on a worker and find it in the API with the
node-name suffix. The kubelet's config points there
(`staticPodPath` in `/var/lib/kubelet/config.yaml`).

## Challenge: The Workload Drills {#workloads-checkpoint points=15}

All six drills done by hand on your cluster: dry-run-generated
deployment, rollout + undo, requests/limits + probes, configmap and
secret consumed both ways, all three placement mechanisms, and a static
pod on a worker.

### Starter

```python
# Self-attested checkpoint — flip DONE once you've honestly done the work.

# I completed every drill in this lesson on my cluster, generating YAML
# with --dry-run=client -o yaml rather than writing it from scratch.
DONE = False

# For your own record: which drill was slowest — that's your review list.
NOTES = """
"""
```

### Tests

```python
from solution import DONE


def test_checkpoint_claimed():
    assert DONE is True, "flip DONE to True once all six drills are done"
```

# Lesson: Services, DNS, and Network Policy {#services-networking}

Pods are ephemeral and their IPs churn; Services are the stable layer on
top, and 20% of the exam lives here.

**The three Service types**, tried in order against a 2-replica
deployment:

- **ClusterIP** (default): a virtual IP reachable only inside the
  cluster. `kubectl expose deployment web --port 80` and note the IP.
- **NodePort**: ClusterIP plus a high port (30000–32767) opened on
  *every node*. `curl http://192.168.1.55:<nodeport>` from your
  workstation — that's the no-cloud-LB way in.
- **LoadBalancer**: on clouds, provisions a real LB. On your bare-metal
  cluster it sits `<pending>` forever — *know why*: nothing implements
  the provisioning (that's what MetalLB, or k3s's servicelb, add).

**DNS.** CoreDNS gives every Service a name:
`<svc>.<namespace>.svc.cluster.local`. Verify the whole resolution chain
with a scratch pod — this is also your standard connectivity-debug move:

```bash
kubectl run tmp --image=busybox:1.36 --rm -it --restart=Never \
  -- nslookup web.default.svc.cluster.local
```

Behind the name: the Service selects pods by label, and matching pods'
addresses appear in **EndpointSlices** (`kubectl get endpointslices`). A
Service with no endpoints means labels don't match — top-three exam
debugging scenario; break it on purpose (change the selector) and watch
endpoints empty out.

**Ingress and Gateway API.** An Ingress routes external HTTP by
host/path to Services, but needs a controller running (install
ingress-nginx from its docs, or note that exam clusters come with what
the task needs). Write one Ingress mapping a hostname to your `web`
service and curl it with a `Host:` header through the controller's
NodePort. The newer **Gateway API** (`GatewayClass` → `Gateway` →
`HTTPRoute`) is on the current curriculum — read a `HTTPRoute` example
and be able to say which object replaces which Ingress part.

**NetworkPolicy** — the reason we chose Calico. Default is allow-all;
production default should be deny, then allow specifics. Apply a
default-deny to a namespace and prove the scratch-pod curl now fails;
then allow only pods labeled `role=frontend` and prove exactly they get
through:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: default-deny-ingress
spec:
  podSelector: {}
  policyTypes: [Ingress]
```

Selectors compose (`podSelector`, `namespaceSelector`, ports) — build
one allow-rule of each kind.

## Challenge: The Networking Drills {#networking-checkpoint points=15}

Service types exercised (including explaining the pending LoadBalancer),
DNS chain verified and broken-selector debugging done, an Ingress
routing by hostname, and a default-deny NetworkPolicy with a working
allow exception.

### Starter

```python
# Self-attested checkpoint — flip DONE once you've honestly done the work.

# ClusterIP/NodePort exercised, empty-EndpointSlice debugging done, an
# Ingress routes to my service, and my default-deny + allow-frontend
# NetworkPolicy pair provably works both ways.
DONE = False

# For your own record: your standard debug pod one-liner.
NOTES = """
"""
```

### Tests

```python
from solution import DONE


def test_checkpoint_claimed():
    assert DONE is True, "flip DONE to True once the drills all passed"
```

# Lesson: Storage {#storage}

The smallest domain (10%), with a small concept count to match. The
model is a supply chain: a **PersistentVolume** (PV) is storage that
exists; a **PersistentVolumeClaim** (PVC) is a request for some; pods
mount claims, never volumes. The decoupling lets app manifests stay
ignorant of what disks the cluster actually has.

**Static provisioning** — you handcraft both ends. Make a PV backed by a
directory on a specific worker, claim it, mount it:

```yaml
apiVersion: v1
kind: PersistentVolume
metadata:
  name: pv-manual
spec:
  capacity:
    storage: 1Gi
  accessModes: [ReadWriteOnce]
  persistentVolumeReclaimPolicy: Retain
  hostPath:
    path: /mnt/data
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: claim-manual
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 500Mi
  storageClassName: ""
```

Watch the claim bind (`kubectl get pv,pvc`), mount it in a pod at
`/data`, write a file, delete the pod, remount, and find the file — the
entire point of the abstraction, witnessed. The details the exam probes:
**accessModes** (`ReadWriteOnce` = one *node* — not one pod;
`ReadOnlyMany`; `ReadWriteMany` — needs storage that can do it, e.g.
NFS), **reclaimPolicy** (`Retain` keeps released data for an admin;
`Delete` removes it with the claim), and binding rules (a claim binds a
PV that's big enough with compatible modes — a 2Gi claim against your
1Gi PV stays `Pending` forever; try it).

**Dynamic provisioning** — a **StorageClass** names a provisioner, and
claims that reference the class get a PV made to order; `kubectl get sc`
on clouds shows a default class, which is why most PVCs in the wild
never mention PVs at all. Your bare cluster has no provisioner — install
the `local-path-provisioner` (Rancher's, the same one k3s bundles) from
its README, make its class the default, and watch a class-based PVC go
`Pending → Bound` the moment a pod uses it.

## Challenge: The Storage Drills {#storage-checkpoint points=10}

Static PV/PVC bound and remounted with data surviving a pod, an
impossible claim diagnosed, and dynamic provisioning working via an
installed StorageClass.

### Starter

```python
# Self-attested checkpoint — flip DONE once you've honestly done the work.

# My handmade PV bound to its claim and data survived pod deletion; I
# watched an oversize claim stay Pending and knew why; local-path is my
# default StorageClass and dynamically provisioned a volume.
DONE = False

# For your own record: reclaim policy behavior you observed on delete.
NOTES = """
"""
```

### Tests

```python
from solution import DONE


def test_checkpoint_claimed():
    assert DONE is True, "flip DONE to True once storage drills are done"
```

# Lesson: RBAC — Who Can Do What {#rbac}

Every request to the API server carries an identity (a user from a
client certificate, or a workload's **ServiceAccount**), and RBAC
decides what that identity may do. Four objects, one 2×2 grid:
**Role**/**ClusterRole** define permission bundles (namespaced /
cluster-wide), **RoleBinding**/**ClusterRoleBinding** attach them to
identities. Rules are verbs on resources:

```bash
kubectl create role pod-reader --verb=get,list,watch --resource=pods -n dev
kubectl create rolebinding alice-reads-pods --role=pod-reader --user=alice -n dev
```

Your verification tool — learn it before creating anything, because it's
also how you *check your own work* on the exam:

```bash
kubectl auth can-i list pods -n dev --as alice        # yes
kubectl auth can-i delete pods -n dev --as alice      # no
kubectl auth can-i list pods -n default --as alice    # no — Role is namespaced
```

**Make alice real.** Kubernetes has no user database; a user is anyone
holding a client cert signed by the cluster CA with `CN=<username>`.
kubeadm can mint a complete kubeconfig in one line — do it, then repeat
the can-i checks *as* alice with `--kubeconfig`:

```bash
sudo kubeadm kubeconfig user --client-name alice > alice.conf
kubectl --kubeconfig alice.conf get pods -n dev       # works
kubectl --kubeconfig alice.conf get pods -n default   # Forbidden
```

(The long-form flow — openssl key + CSR, a `CertificateSigningRequest`
object, `kubectl certificate approve` — is in the docs under "Certificate
Signing Requests"; walk it once so the objects are familiar.)

**ServiceAccounts** are the same machinery for pods: create one, bind it
a Role, set `serviceAccountName` in a pod spec, and from inside the pod
watch the mounted token be honored and limited:

```bash
kubectl create serviceaccount app-sa
kubectl create rolebinding app-sa-reads --role=pod-reader --serviceaccount=default:app-sa
```

The common exam shapes: "create an SA that can only X", "why can't this
pod list services?" (`can-i --as system:serviceaccount:<ns>:<sa>`), and
"give user U read access to namespace N."

## Challenge: The Least-Privilege Drills {#rbac-checkpoint points=10}

A namespaced read-only user minted and verified from both directions
(can-i as admin, real requests as alice), and a ServiceAccount-bound
role honored from inside a pod.

### Starter

```python
# Self-attested checkpoint — flip DONE once you've honestly done the work.

# alice exists as a real kubeconfig, can list pods only in dev; an SA
# with a bound role works from inside a pod; I verified everything with
# kubectl auth can-i --as before and after.
DONE = False

# For your own record: the can-i incantation for service accounts.
NOTES = """
"""
```

### Tests

```python
from solution import DONE


def test_checkpoint_claimed():
    assert DONE is True, "flip DONE to True once alice is properly limited"
```

# Lesson: Cluster Lifecycle — Upgrades, Backups, Maintenance {#cluster-lifecycle}

The tasks in this lesson are the highest-stakes ones an administrator
does, which is precisely why the exam loves them: they're procedures,
and procedures reward practice.

## etcd backup and restore — the marquee exam task

The whole task is two commands with a file in between: `snapshot save`
freezes cluster state to a `.db`; `snapshot restore` replays it into a
fresh data-dir.

```d2
direction: right
live: "live etcd\ncluster state" {shape: cylinder; style.stroke: "#34d399"; style.stroke-width: 2}
snap: "snapshot.db" {shape: page; style.stroke: "#d97706"; style.stroke-width: 2}
restored: "restored etcd\nnew data-dir" {shape: cylinder}
live -> snap: "snapshot save"
snap -> restored: "snapshot restore"
```

All cluster state lives in etcd, so a cluster backup *is* an etcd
snapshot. On `cp-1` (etcd speaks TLS with certs from the pki directory —
all three flags required, a fact the exam checks by omission):

```bash
sudo apt install -y etcd-client
sudo ETCDCTL_API=3 etcdctl snapshot save /var/backups/etcd-snap.db \
  --endpoints=https://127.0.0.1:2379 \
  --cacert=/etc/kubernetes/pki/etcd/ca.crt \
  --cert=/etc/kubernetes/pki/etcd/server.crt \
  --key=/etc/kubernetes/pki/etcd/server.key
```

Restore inverts it: unpack the snapshot to a *new* data directory, then
point etcd's static pod at it:

```bash
sudo etcdutl snapshot restore /var/backups/etcd-snap.db \
  --data-dir /var/lib/etcd-restore
```

then edit `/etc/kubernetes/manifests/etcd.yaml`'s hostPath volume from
`/var/lib/etcd` to `/var/lib/etcd-restore` and let the kubelet restart
it. Drill the full loop: create a marker deployment, snapshot, delete
the deployment, restore, watch the deployment *come back*. (On your HA
cluster a true restore involves all three members — practice the loop on
one and read the multi-member procedure in the docs; better yet, drill
it before the HA lesson's snapshot of state, or on a scratch single-CP
cluster.)

## Upgrading — one minor, one node at a time

Here's why node-prep pinned you a minor behind. The order is inviolable:
control planes first, workers after; and the apt repo is per-minor, so
step one is editing `kubernetes.list` from `v1.34` to `v1.35`. On the
first control plane:

```bash
sudo sed -i 's/v1.34/v1.35/' /etc/apt/sources.list.d/kubernetes.list
sudo apt update
sudo apt-mark unhold kubeadm && sudo apt install -y kubeadm='1.35.*' && sudo apt-mark hold kubeadm
sudo kubeadm upgrade plan          # read it — it tells you exactly what it will do
sudo kubeadm upgrade apply v1.35.x
```

Then that node's kubelet, behind a drain:

```bash
kubectl drain cp-1 --ignore-daemonsets
sudo apt-mark unhold kubelet kubectl \
  && sudo apt install -y kubelet='1.35.*' kubectl='1.35.*' \
  && sudo apt-mark hold kubelet kubectl
sudo systemctl daemon-reload && sudo systemctl restart kubelet
kubectl uncordon cp-1
```

Remaining control planes run `sudo kubeadm upgrade node` (not `apply` —
another exam-checked distinction) plus the same kubelet dance; workers
likewise. `kubectl get nodes` mid-upgrade shows a version skew — that's
normal and supported within one minor.

**Drain semantics**, since you just used them: `cordon` marks a node
unschedulable; `drain` cordons *and evicts* pods so controllers recreate
them elsewhere; `--ignore-daemonsets` acknowledges DaemonSet pods can't
move. `uncordon` reopens. This trio is also how any node maintenance
starts and ends.

## Certificates age

kubeadm's component certs live one year. `kubeadm certs
check-expiration` shows the clock; `kubeadm certs renew all` (then
restart the static pods) resets it. Upgrades renew certs as a side
effect — clusters that never upgrade are the ones that die of cert
expiry, a fact that explains several real-world outages and one classic
exam troubleshooting setup.

## Challenge: The Lifecycle Drills {#lifecycle-checkpoint points=15}

The etcd save/delete/restore loop proven with a marker object, the full
cluster upgraded one minor (all five nodes, correct order, drains
included), and cert expiries checked.

### Starter

```python
# Self-attested checkpoint — flip DONE once you've honestly done the work.

# My deleted marker deployment came back from an etcd restore; every node
# now runs the next minor via kubeadm upgrade apply/node with drains;
# kubeadm certs check-expiration output makes sense to me.
DONE = False

# For your own record: total upgrade wall-clock; anything that surprised
# you during drain.
NOTES = """
"""
```

### Tests

```python
from solution import DONE


def test_checkpoint_claimed():
    assert DONE is True, "flip DONE to True once restore and upgrade worked"
```

# Lesson: Helm, Kustomize, and Extension Points {#helm-kustomize}

The current curriculum expects working fluency with the two standard
ways of managing manifest complexity, plus awareness of how Kubernetes
extends itself.

**Helm** — templated charts with versioned releases. Install it
(one-line script in the Helm docs), then run one full lifecycle against
your cluster:

```bash
helm repo add bitnami https://charts.bitnami.com/bitnami
helm repo update
helm install mydb bitnami/postgresql --set auth.postgresPassword=drill
helm list
helm get values mydb
helm upgrade mydb bitnami/postgresql --set primary.persistence.size=2Gi
helm rollback mydb 1
helm uninstall mydb
```

The exam shapes: install a given chart with specific values overridden
(`--set` or `-f values.yaml`), upgrade a release, roll one back. Notice
Helm tracks *releases* — state about what it installed — which is what
makes rollback possible.

**Kustomize** — no templates, no state: a base of plain manifests plus
overlays that patch them, built into kubectl. Make the exam-typical
layout with your `web.yaml` as base:

```text
base/            kustomization.yaml + web.yaml
overlays/prod/   kustomization.yaml   (namePrefix: prod-, replicas: 5)
```

```bash
kubectl kustomize overlays/prod    # renders — inspect before touching the cluster
kubectl apply -k overlays/prod
```

Know when each fits: Helm for third-party software with someone else's
knobs; Kustomize for your own manifests varied per environment.

**Extension awareness** (know-what-it-is depth): **CRDs** add new object
types — you already have some (`kubectl get crd` shows Calico's), and an
**operator** is a controller reconciling a CRD, which is exactly what
the tigera-operator you installed does. `kubectl api-resources` is the
census of everything your cluster can store.

## Challenge: The Packaging Drills {#helm-kustomize-checkpoint points=10}

One full Helm release lifecycle including a values override and a
rollback, and a Kustomize base/overlay applied with a rendered preview
first — plus naming which CRDs your cluster already carries.

### Starter

```python
# Self-attested checkpoint — flip DONE once you've honestly done the work.

# helm install/upgrade/rollback/uninstall all ran with custom values; my
# prod overlay renders and applies with a prefix and replica bump; I can
# name the CRDs Calico added.
DONE = False

# For your own record: helm vs kustomize — your one-sentence rule of thumb.
NOTES = """
"""
```

### Tests

```python
from solution import DONE


def test_checkpoint_claimed():
    assert DONE is True, "flip DONE to True once both tools feel routine"
```

# Lesson: Troubleshooting — the 30% {#troubleshooting}

The biggest exam domain rewards a *method*, not memorized fixes. This is
the method; then you'll sabotage your own cluster until the method is
reflex.

**The workload ladder** — for anything broken above the node line:

```bash
kubectl get pods -o wide                     # what state, which node?
kubectl describe pod <p>                      # THE command: events live here
kubectl logs <p> [-c container] [--previous]  # app said what? before dying?
kubectl get events --sort-by=.lastTimestamp   # cluster-wide recent history
```

Map states to causes cold: **Pending** = unschedulable (insufficient
requests, taints without tolerations, selector matches nothing — the
describe events say which) or an unbindable PVC. **ImagePullBackOff** =
name/tag/registry/secret. **CrashLoopBackOff** = it *starts* then dies —
read `logs --previous`, check probes against real ports (you planted
exactly this bug in the workloads lesson). **Running but useless** =
usually Service selector/EndpointSlice, or a NetworkPolicy you forgot.

**The node ladder** — for `NotReady`, work *up the stack on the node*:

```bash
kubectl describe node w-1                    # conditions block first
ssh w-1
systemctl status kubelet && sudo journalctl -u kubelet --since -10m
systemctl status containerd
sudo crictl ps -a                            # the runtime's view, no API needed
```

`crictl` matters because it works when the API server doesn't — it's how
you debug a broken *control plane* from the inside, inspecting the etcd
and apiserver containers directly. For control-plane failures add:
static pod manifests (`/etc/kubernetes/manifests` — typos there are a
beloved exam setup), and `kubeadm certs check-expiration`.

## Sabotage drills

Do each break, then fix it *using only the ladders* — no rereading the
lesson that created the thing. Better: have someone (or an AI agent)
break the cluster for you blind.

1. `systemctl stop kubelet` on `w-2`; explain the exact `get nodes` /
   `get pods` symptoms before fixing.
2. Change the `image:` in `kube-apiserver.yaml` on one control plane to
   a nonexistent tag. (HA note: kubectl still answers via the others —
   *finding* the sick member is the drill. `crictl` is your friend.)
3. `kubectl -n kube-system scale deployment coredns --replicas=0`, then
   debug "my pods can't resolve anything" backwards from a scratch pod.
4. Deploy a pod with requests larger than any node; read the scheduler's
   complaint from events.
5. Break a Service by editing its selector to match nothing; diagnose
   from the empty EndpointSlice.
6. Set a liveness probe to the wrong port; watch the restart counter
   climb and explain the CrashLoop timeline from events.

## Challenge: The Sabotage Gauntlet {#troubleshooting-checkpoint points=20}

All six sabotage drills broken and repaired using the ladders, with the
symptom → cause → fix chain narrated (out loud or in notes) each time.

### Starter

```python
# Self-attested checkpoint — flip DONE once you've honestly done the work.

# I broke and fixed all six scenarios, diagnosing from describe/events/
# logs/journalctl/crictl rather than from memory of what I broke.
DONE = False

# For your own record: your personal triage order, written as a recipe.
NOTES = """
"""
```

### Tests

```python
from solution import DONE


def test_checkpoint_claimed():
    assert DONE is True, "flip DONE to True once the gauntlet is beaten"
```

# Final Challenge: The Mock Exam {#mock-exam points=50}

Simulate the real thing. **Two hours on a timer**, docs allowed (only
kubernetes.io/docs, kubernetes.io/blog, and helm.sh — the real
allowlist), no lesson text, no old shell history. Do the tasks in any
order; skip and return rather than sinking time — that discipline is
half of passing. Score a task only if it fully works.

1. Create namespace `mock` and, in it, a deployment `api` with 3
   replicas of `nginx:1.27`, requests of 100m CPU / 128Mi memory.
2. Roll `api` to `nginx:1.28`, verify, then roll it back.
3. Expose `api` on a NodePort and curl it from outside the cluster.
4. Write a NetworkPolicy in `mock` denying all ingress except from pods
   labeled `role=client`; prove both directions.
5. Create a PV (1Gi, hostPath, Retain) and a PVC that binds it; mount it
   in a pod and write a file that survives pod deletion.
6. Create a ServiceAccount `auditor` that can `get`/`list` pods
   cluster-wide and nothing else; verify with `auth can-i` both ways.
7. Taint `w-1` with `team=infra:NoSchedule` and deploy one pod that can
   land there and one that provably can't.
8. Run a static pod named `heartbeat` (busybox, `sleep infinity`) on
   `w-2`.
9. Snapshot etcd; delete namespace `mock`; restore the snapshot and show
   the namespace's contents returned.
10. A friend (or you, eyes closed) stops the kubelet and breaks one
    static-pod manifest on a node of their choosing: find and fix both.
11. Install any small Helm chart with one overridden value; then roll it
    back after an upgrade.
12. Build a Kustomize overlay that renames everything with prefix
    `mock-` and doubles `api`'s replicas; apply it.

Passing is 8 of 12 (≈66%), but the real deliverable is your gap list:
whatever you skipped or fumbled tells you exactly which lesson to
re-drill before booking the real exam — with its included killer.sh
sessions as the dress rehearsal.

### Starter

```python
# Self-attested checkpoint — flip each flag once it's honestly true.

# I ran the mock as written: 2h timer, docs-only, no lesson text.
RAN_UNDER_EXAM_CONDITIONS = False

# I fully completed at least 8 of the 12 tasks within the time.
SCORED_A_PASS = False

# I wrote down my gap list and re-drilled the weak lessons.
GAPS_DRILLED = False

# For your own record: score, time remaining, gap list.
NOTES = """
"""
```

### Tests

```python
from solution import GAPS_DRILLED, RAN_UNDER_EXAM_CONDITIONS, SCORED_A_PASS


def test_ran_under_exam_conditions():
    assert RAN_UNDER_EXAM_CONDITIONS is True


def test_scored_a_pass():
    assert SCORED_A_PASS is True


def test_gaps_drilled():
    assert GAPS_DRILLED is True
```
