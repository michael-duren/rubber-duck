---
course: proxmox-k8s-homelab
title: Kubernetes Homelab on Proxmox
language: python
description: >
  Turn two physical machines into a real Kubernetes cluster the
  infrastructure-as-code way. Install Proxmox VE, drive it with Terraform
  and cloud-init so every VM is reproducible from a git repo, baseline the
  VMs with Ansible — then bootstrap k3s: a control plane on your main box,
  dedicated workers carved out of the second, and a first workload served
  through ingress. Checkpoints are self-attested: you do the work on your
  own hardware and claim the points.
duration_hours: 16
tags: [infrastructure, homelab, kubernetes, devops]
extended_reading:
  - title: Proxmox VE Administration Guide
    url: https://pve.proxmox.com/pve-docs/pve-admin-guide.html
  - title: bpg/proxmox Terraform provider docs
    url: https://registry.terraform.io/providers/bpg/proxmox/latest/docs
  - title: Cloud-init documentation
    url: https://cloudinit.readthedocs.io/en/latest/
  - title: Ansible community.general Proxmox modules
    url: https://docs.ansible.com/ansible/latest/collections/community/general/proxmox_kvm_module.html
  - title: k3s documentation
    url: https://docs.k3s.io
  - title: Kubernetes concepts
    url: https://kubernetes.io/docs/concepts/
---

# Lesson: The Blueprint — Two Nodes, One Cluster {#blueprint}

By the end of this course you will have a Kubernetes cluster running on
hardware you own, built in a way you can tear down and rebuild from a git
repo. This first section lays the foundation: Proxmox on your server, and
every VM on it defined in code.

**A note on how this course grades.** Unlike most courses on this platform,
your work happens on real machines, not in a code runner. Each checkpoint
is *self-attested*: do the work, then flip `DONE = False` to `DONE = True`
in the starter code and submit. Nothing is validated — the points are yours
when you can honestly claim them. The `NOTES` fields are for your own
record; paste in whatever future-you will thank you for.

## The target architecture

Two physical machines, both running Proxmox VE (a bare-metal hypervisor):

- **`pve-main`** — your main box. It already earns its keep running
  Jellyfin and other services, and it keeps doing that. Alongside them it
  hosts exactly **one** Kubernetes VM: `k8s-cp-1`, the **control plane**
  node that runs the cluster's brain (API server, scheduler, etcd).
- **`pve-worker`** — a second machine dedicated to the cluster. Every VM
  on it is a Kubernetes **worker** node (`k8s-w-1`, `k8s-w-2`, …) that
  actually runs your workloads.

The control plane VM on `pve-main` manages the worker VMs on `pve-worker`
over your home network. Kubernetes doesn't care that its nodes live on two
different hypervisors — to it they're just machines that can reach each
other.

## Why VMs on Proxmox instead of Kubernetes on bare metal?

You could install Kubernetes directly on both machines. Don't — at least
not for a homelab you're also living on:

- **Isolation.** Jellyfin and friends stay untouched no matter how badly a
  Kubernetes experiment goes. Worst case you delete a VM, not your media
  server.
- **Cattle, not pets.** A VM defined in Terraform can be destroyed and
  recreated in minutes. Bare metal can't. When (not if) you wedge a node
  learning Kubernetes, `terraform apply` is your undo button.
- **Snapshots.** About to try something sketchy? Snapshot the VM first.
- **Growing room.** More workers later is a copy-paste in a `.tf` file,
  not another trip to the BIOS.

## What you need

- Two x86-64 machines that can run Proxmox (VT-x/AMD-V enabled in the
  BIOS). 16 GB+ RAM each is comfortable; the main box wants more since it
  also runs your services. The second machine can arrive later — this
  whole section is doable with just `pve-main`.
- A workstation (your daily driver) where you'll run `terraform`,
  `ansible`, and `ssh`. Your Proxmox nodes are servers; you never work *on*
  them, you work *against* them.
- A USB stick (1 GB+) for the installer.
- A home network where you control the router, so you can carve out a
  block of static IPs.

## Plan your addresses now

Everything that follows goes smoother with an IP plan up front. Pick a
block *outside* your router's DHCP range (check the router's admin page).
Example on a `192.168.1.0/24` network:

| Host       | IP             | Role                                |
|------------|----------------|-------------------------------------|
| `pve-main` | `192.168.1.10` | Proxmox node 1 — services + control plane VM |
| `pve-worker` | `192.168.1.11` | Proxmox node 2 — worker VMs       |
| `k8s-cp-1` | `192.168.1.60` | Kubernetes control plane (VM)       |
| `k8s-w-1`  | `192.168.1.61` | Kubernetes worker (VM)              |
| `k8s-w-2`  | `192.168.1.62` | Kubernetes worker (VM)              |

Also reserve a VM ID plan (Proxmox gives every VM a numeric ID): `9000`
for templates, `200` for the control plane, `210+` for workers. IDs are
arbitrary, but a convention keeps the UI readable a year from now.

Substitute your own subnet everywhere the course shows `192.168.1.x`.

## Lay out the repo

Create the git repo now, on your workstation, and give every future file a
home before any tool asks for one. Three tools will write files into this
repo — Terraform, Ansible, and `kubectl` manifests — and they stay sane in
separate directories:

```bash
mkdir homelab && cd homelab
git init
mkdir infra ansible manifests
```

```text
homelab/
├── README.md      # your blueprint — this lesson's challenge
├── infra/         # Terraform: *which VMs exist* (the Terraform lessons)
├── ansible/       # inventory + playbooks: *what's installed on them*
└── manifests/     # Kubernetes YAML: *what runs on the cluster* (last section)
```

(Git doesn't track empty directories — they'll show up in `git status`
once the first file lands in each.) From here on, when a lesson says
"create `main.tf`" it means `infra/main.tf`, playbooks go in `ansible/`,
and Kubernetes YAML goes in `manifests/`. The lessons spell the full path
out each time.

## Challenge: Commit to the Blueprint {#blueprint-checkpoint points=5}

Write down your plan: hostnames, IPs, VM IDs, and which machine plays
which role. Put it in the repo's `README.md` — that repo is about to
accumulate Terraform and Ansible code and becomes the single source of
truth for your lab. Then fill in the same routing table in the starter
below with your real values: your router's IP, its DHCP range, and an
address + VM ID for every machine in the blueprint.

### Starter

```python
# Self-attested checkpoint — flip DONE once you've honestly done the work.

# I created the homelab repo with infra/, ansible/ and manifests/
# directories, and my README holds the plan I filled in below.
DONE = False

# Fill in your routing table — the same plan as the lesson's, with your
# network's real numbers. The static IPs must sit OUTSIDE the DHCP range.
PLAN = """
| host       | ip          | vm id | role                                    |
|------------|-------------|-------|-----------------------------------------|
| router     |             |   —   | gateway; DHCP range: ___ - ___          |
| pve-main   |             |   —   | hypervisor: services + control plane VM |
| pve-worker |             |   —   | hypervisor: worker VMs                  |
| k8s-cp-1   |             |       | Kubernetes control plane (VM)           |
| k8s-w-1    |             |       | Kubernetes worker (VM)                  |
| k8s-w-2    |             |       | Kubernetes worker (VM)                  |
"""
```

### Tests

```python
from solution import DONE


def test_checkpoint_claimed():
    assert DONE is True, "flip DONE to True once the blueprint repo exists"
```

# Lesson: Installing Proxmox VE {#install-proxmox}

Proxmox VE is a Debian-based OS whose whole job is running VMs and
containers. You install it *instead of* a regular Linux distro, boot it
headless, and manage everything from a web UI and an API.

## Make the installer USB

Download the latest Proxmox VE ISO from
[proxmox.com/downloads](https://www.proxmox.com/en/downloads). Write it to
the USB stick from your workstation — on Linux that's `dd` (double-check
the device name with `lsblk` first; `dd` to the wrong disk is
unrecoverable):

```bash
lsblk                                   # find your USB stick, e.g. /dev/sdb
sudo dd if=proxmox-ve_*.iso of=/dev/sdb bs=4M status=progress conv=fsync
```

## Run the installer

Boot the server from the USB stick (usually a boot-menu key like F11/F12
at power-on). The installer asks only a handful of questions:

- **Target disk & filesystem.** For a single-disk machine the default
  (ext4 on LVM) is fine and is what this course assumes. ZFS is the better
  choice if you have two disks to mirror — worth a detour on your NAS
  someday, a distraction today.
- **Country / timezone / password.** The password is for the `root` user —
  both SSH and the web UI. Use a real one; this box will hold API tokens.
- **Management network.** This is the important screen. Give the machine
  the **static IP from your blueprint** (`192.168.1.10/24` for
  `pve-main`), your router as gateway, and an FQDN like
  `pve-main.home.lan`. The hostname part before the first dot becomes the
  node name you'll see everywhere — in the UI, in Terraform, in the API.

The machine reboots into a console that prints its management URL. You're
done at the physical console — unplug the monitor; everything else in this
course happens from your workstation.

## First login and updates

Browse to `https://192.168.1.10:8006` (note the port, and accept the
self-signed-certificate warning — it's your own box). Log in as `root`
with realm **Linux PAM**.

You'll immediately hit the "No valid subscription" dialog. Proxmox VE is
fully open source; the paid subscription buys you the *enterprise* package
repository and support. For a homelab, switch to the free
**no-subscription** repository: in the UI go to your node →
**Updates → Repositories**, **Disable** the `enterprise` entries, then
**Add → No-Subscription**. (Under the hood this edits APT source files in
`/etc/apt/sources.list.d/` — it's still just Debian.)

Then update, either from **Updates → Refresh/Upgrade** or over SSH:

```bash
ssh root@192.168.1.10
apt update && apt full-upgrade -y
```

## The network bridge: how VMs get on your LAN

One piece of the installer's work is worth understanding now, because
Terraform will reference it later. Two terms first:

- A **NIC** (network interface card) is a machine's physical network
  port — the thing the Ethernet cable plugs into. Linux names NICs by
  where they sit on the motherboard, so yours will be something like
  `enp3s0` or `eno1`.
- A **Linux bridge** is a virtual network switch that lives inside the
  kernel. Like a physical switch, it has ports and forwards traffic
  between them — but its ports connect *interfaces* (real or virtual)
  instead of cables.

The installer created a bridge called `vmbr0` and plugged your physical
NIC into it. Every VM you create gets a *virtual* NIC plugged into
another port of the same bridge. Result: VMs appear on your home LAN as
first-class machines with their own IPs, no NAT involved — to your
router, a VM is just one more device on the network.

See it for yourself. SSH to the node and print its network config:

```bash
ssh root@192.168.1.10
cat /etc/network/interfaces
```

Line by line (yours will name a different physical NIC):

```text
auto lo                     # bring "lo" up automatically at boot
iface lo inet loopback      # lo is the loopback device — 127.0.0.1

iface enp3s0 inet manual    # the physical NIC; "manual" = it gets no IP
                            # of its own, it only feeds the bridge

auto vmbr0                  # bring the bridge up at boot
iface vmbr0 inet static     # the bridge holds a fixed ("static") IP...
    address 192.168.1.10/24 # ...the management IP you gave the installer
    gateway 192.168.1.1     # your router — where non-local traffic goes
    bridge-ports enp3s0     # the physical NIC plugged into this switch
    bridge-stp off          # spanning-tree protocol: loop detection for
                            # networks of many switches; pointless here
    bridge-fd 0             # forwarding delay: seconds to wait before
                            # passing traffic — 0, don't wait
```

Note the node's own IP lives on the *bridge*, not the NIC — the NIC is
`manual`, just a dumb uplink. You don't need to edit any of this — the
installer got it right — but when a VM config says `bridge=vmbr0`, this
is the switch it's plugging into.

If you have the second machine already, repeat this lesson on it now
(`pve-worker`, `192.168.1.11`). If not, carry on — everything until the
Kubernetes section works with one node.

## Challenge: A Node You Can Log Into {#install-checkpoint points=15}

Proxmox VE installed on your main machine with the static IP from your
blueprint, enterprise repos swapped for no-subscription, fully updated,
and the web UI reachable from your workstation.

### Starter

```python
# Self-attested checkpoint — flip DONE once you've honestly done the work.

# Proxmox VE is installed on pve-main with a static IP, repos switched to
# no-subscription, apt full-upgrade clean, and I can log into the web UI
# on :8006 from my workstation.
DONE = False

# For your own record: node name, IP, PVE version (see the UI top-left).
NOTES = """
"""
```

### Tests

```python
from solution import DONE


def test_checkpoint_claimed():
    assert DONE is True, "flip DONE to True once the node is up and updated"
```

# Lesson: An API Built for Automation {#api-tokens}

Everything the Proxmox web UI does — every click — is a call to a REST API
listening on the same port 8006. That's not trivia; it's the reason this
whole course works. Terraform and Ansible will drive your hypervisor
through that API, so first we give them an identity to do it with.

## Users, realms, and why not root

Proxmox authenticates users against *realms* — separate databases of who
can log in. The `USER@REALM` suffix you saw on the login screen names
which database a user lives in:

- **`@pam`** — **PAM** is *Pluggable Authentication Modules*, the
  standard mechanism Linux itself uses for logins. Users in this realm
  are real Linux accounts on the node; `root@pam` is the same `root` you
  SSH in as.
- **`@pve`** — **PVE** is just *Proxmox Virtual Environment*, the
  product's name. Users in this realm exist only inside Proxmox's own
  user database — the OS underneath has never heard of them.

Automation belongs in the PVE realm: a `@pve` user can't log into the box
over SSH, and it can be deleted without touching the OS.

Never feed your root password to a tool. Instead: a dedicated user plus an
**API token** — a revocable credential you can scope, rotate, and keep in
one place.

Create both with `pveum` — the **P**roxmox **VE** **U**ser **M**anager,
the node's CLI for users, permissions, and tokens (everything it does is
also under **Datacenter → Permissions** in the UI). These three commands
run **on the node**, so SSH in first (`ssh root@192.168.1.10`):

```bash
# create the user "terraform" in the pve realm
pveum user add terraform@pve --comment "Terraform/Ansible automation"

# aclmod edits the access-control list: grant terraform@pve the built-in
# Administrator role on "/" — the root of the resource tree, so the grant
# covers every VM, datastore, and network on this node
pveum aclmod / -user terraform@pve -role Administrator

# mint an API token named "tf" belonging to that user; --privsep 0 means
# the token inherits the user's full rights (more on that below)
pveum user token add terraform@pve tf --privsep 0
```

The last command prints the token secret **exactly once**. Store it in
your password manager now — Proxmox only keeps a hash.

Two honest caveats. `Administrator` on `/` is the pragmatic homelab
choice; a least-privilege setup would define a custom role with only the
`VM.*`, `Datastore.*` and `SDN.Use` privileges the provider needs — a good
exercise once everything works. And `--privsep 0` disables *privilege
separation*, which would otherwise let you grant the token *fewer* rights
than its user; with one token for one purpose it has nothing to separate.

## Talk to the API by hand once

The token authenticates as an HTTP header of the form
`PVEAPIToken=USER@REALM!TOKENID=SECRET`. Prove the plumbing works before
handing the token to any tool — and prove it **from your workstation**,
because that's where Terraform will run. Type `exit` to leave the SSH
session on the node, then (substituting your token's real secret for the
`aaaa...` placeholder):

```bash
curl -k -H 'Authorization: PVEAPIToken=terraform@pve!tf=aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee' \
  https://192.168.1.10:8006/api2/json/nodes | python3 -m json.tool
```

(`-k` skips certificate verification — same self-signed cert you accepted
in the browser; `python3 -m json.tool` just pretty-prints the response.)
You should get a JSON list containing your node, with its uptime and
load. Running it from the workstation matters: it proves the token,
the network path, and the API all work from where your tools will live.
When Terraform misbehaves later, this one-liner is your "is it me or the
API?" bisector.

## Challenge: An Identity for Your Tools {#api-checkpoint points=10}

A `terraform@pve` user with an API token, the secret stored somewhere
safe, and a successful `curl` against `/api2/json/nodes` from your
workstation.

### Starter

```python
# Self-attested checkpoint — flip DONE once you've honestly done the work.

# I created terraform@pve, minted an API token, stored the secret in my
# password manager, and hit /api2/json/nodes with curl successfully.
DONE = False

# For your own record: the token *id* (never the secret!), e.g.
# "terraform@pve!tf".
NOTES = """
"""
```

### Tests

```python
from solution import DONE


def test_checkpoint_claimed():
    assert DONE is True, "flip DONE to True once curl returns your node list"
```

# Lesson: Terraform and the Proxmox Provider {#terraform-proxmox}

Terraform inverts how you've probably managed servers so far. Instead of
running commands that *change* things, you write files that *describe* the
end state — "a VM named k8s-cp-1 with 2 cores and 4 GB exists" — and
Terraform figures out what to create, change, or delete to make reality
match. Run it twice, nothing happens the second time. Delete the resource
from the file, Terraform deletes it from the hypervisor. Your git repo
becomes the truth, and the web UI becomes a read-only dashboard.

Install Terraform on your **workstation** (from your package manager or
[developer.hashicorp.com/terraform](https://developer.hashicorp.com/terraform/install)).
Everything here also works verbatim with OpenTofu, the open-source fork —
`tofu init`, `tofu apply`.

## The provider

Terraform itself knows nothing about Proxmox; a **provider** plugin
translates resources into API calls. Providers are named
`publisher/name`, and Proxmox has no official one — the two candidates
are community projects named after their maintainers' GitHub handles.
We use `bpg/proxmox` (bpg is Pavel Boldyrev, its author), the actively
maintained option; you'll also see the older `telmate/proxmox` in
tutorials, which has lagged behind Proxmox releases for years.

Terraform files go in the `infra/` directory you created in the first
lesson. Create `infra/main.tf`:

```hcl
terraform {
  required_providers {
    proxmox = {
      source  = "bpg/proxmox"
      version = "~> 0.70"
    }
  }
}

provider "proxmox" {
  endpoint  = "https://192.168.1.10:8006/"
  api_token = var.proxmox_api_token
  insecure  = true # self-signed cert; pin or ACME it later

  ssh {
    agent    = true
    username = "root"
  }
}
```

The `ssh` block is a quirk of this provider worth knowing: a few
operations (uploading cloud-init snippet files, some disk imports) aren't
fully covered by the Proxmox API, so for those the provider falls back to
SSHing into the node as root and doing the work there. `agent = true`
tells it to authenticate using your **ssh-agent** — the background
process that holds your unlocked private keys so programs can use them
without prompting.

That means key-based SSH from workstation to node has to work *before*
Terraform needs it. Set it up now, from your workstation:

```bash
# 1. if you've never made an SSH key, create one (accept the defaults)
ssh-keygen -t ed25519

# 2. install your public key on the node (enter the root password one
#    last time)
ssh-copy-id root@192.168.1.10

# 3. verify: this must log you in with NO password prompt
ssh root@192.168.1.10 hostname
```

If step 3 still prompts for a *key passphrase*, your agent isn't holding
the key — run `ssh-add` and try again. Once step 3 is silent, the
provider's SSH fallback will be too.

## Secrets stay out of git

That `var.proxmox_api_token` is a Terraform *variable*, declared in
`infra/variables.tf`:

```hcl
variable "proxmox_api_token" {
  description = "Proxmox API token in user@realm!id=secret form"
  type        = string
  sensitive   = true
}
```

Supply the value outside version control, one of two ways:

- A `credentials.auto.tfvars` file next to `main.tf` in `infra/`, which
  you **gitignore**. Terraform automatically loads any `*.auto.tfvars`
  file in the working directory:

  ```hcl
  proxmox_api_token = "terraform@pve!tf=aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
  ```

- An environment variable. Terraform automatically resolves any variable
  named `TF_VAR_<name>` to the Terraform variable `<name>` — no flag, no
  file, nothing to reference. Export it and every `plan`/`apply` in that
  shell just has it:

  ```bash
  export TF_VAR_proxmox_api_token='terraform@pve!tf=aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee'
  ```

Create the repo's `.gitignore` now, with three entries:

```text
*.tfvars
.terraform/
terraform.tfstate*
```

The first is your credentials file; `.terraform/` is just the downloaded
provider cache (bulk, not secret). The third deserves a real explanation,
because it's the one that bites people: the **state file**
(`terraform.tfstate`) is Terraform's record of everything it has created,
and it stores resource attributes **in plaintext — including sensitive
ones like your API token**. `sensitive = true` only redacts values from
Terraform's *output*; it does not encrypt them in state. So treat the
state file itself like a credential:

- It must never land in git — not even a private repo.
- On a single-operator homelab, local state on your workstation's disk
  is an accepted trade-off, but know what you're trading: anyone who can
  read `infra/terraform.tfstate` has your Proxmox token. Keep the repo on
  a disk only you use, and if you back it up, back it up encrypted.
- The production answer is a **remote state backend** (an S3/GCS bucket
  with encryption, or Terraform Cloud): state lives encrypted at rest,
  off your laptop, with locking so two applies can't collide. It's worth
  doing the day a second person — or a CI job — touches this repo; it's
  ceremony you don't need today.

## init, plan, apply

Terraform operates on the `.tf` files in the *current directory*, so
these commands always run from `infra/`. The workflow you'll run hundreds
of times:

```bash
cd infra
terraform init      # downloads the provider; once per repo (and after version bumps)
terraform plan      # dry run: shows what WOULD change, changes nothing
terraform apply     # shows the plan again, asks, then does it
```

Let's manage something real but harmless first — a **resource pool**, a
folder-like grouping in the Proxmox UI that our cluster VMs will live in.
Add to `infra/main.tf`:

```hcl
resource "proxmox_virtual_environment_pool" "k8s" {
  pool_id = "k8s"
  comment = "Kubernetes cluster VMs — managed by Terraform, do not hand-edit"
}
```

`terraform plan` should announce `1 to add`. Apply it, then refresh the
web UI: there's your pool, created by code. Now feel the loop from the
other side — change the `comment`, plan, and watch Terraform propose an
in-place update; or delete the block entirely and watch the plan propose
destroying it (put it back — we need it).

That loop — edit file, plan, read the plan *carefully*, apply — is the
entire discipline of infrastructure-as-code. The plan step is what makes
Terraform safe to point at a machine that also runs your media server:
nothing happens that you didn't read first.

## Challenge: Reality From a File {#terraform-checkpoint points=15}

A homelab repo with the bpg provider configured, secrets gitignored, and
the `k8s` pool created by `terraform apply` — plus one round-trip of
changing something, planning, and applying to feel the loop.

### Starter

```python
# Self-attested checkpoint — flip DONE once you've honestly done the work.

# terraform init/plan/apply works against my node with an API token from
# a gitignored tfvars/env var, and the "k8s" pool in the web UI was
# created by Terraform, not by me clicking.
DONE = False

# For your own record: provider version pinned, where the token lives.
NOTES = """
"""
```

### Tests

```python
from solution import DONE


def test_checkpoint_claimed():
    assert DONE is True, "flip DONE to True once apply created the pool"
```

# Lesson: Cloud-Init Templates and Your First VM as Code {#cloud-init-template}

Creating a VM the traditional way — boot an ISO, click through an OS
installer, set a password — takes twenty minutes of babysitting per VM.
That doesn't scale to "destroy the cluster and rebuild it before dinner."
The cloud solved this years ago with two pieces we're about to steal:

- **Cloud images**: pre-installed, generic disk images distros publish for
  exactly this purpose. No installer; the OS is already on the disk.
- **cloud-init**: a service baked into those images that runs on first
  boot and asks its environment "who am I?" — then sets the hostname, IP,
  users, and SSH keys it's told. Proxmox speaks cloud-init natively: VM
  settings become a tiny config disk the image reads at boot.

Combine them into a **template** — a frozen golden VM — and every future
VM is a clone of it plus a cloud-init config. Clone-and-boot takes
seconds, and nobody types passwords into consoles ever again.

## Build the template (once per node)

SSH to the node. `qm` is Proxmox's VM management CLI — same API the web UI
and Terraform use:

```bash
# 1. fetch Debian's official cloud image
wget https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-generic-amd64.qcow2

# 2. create an empty VM shell (ID 9000 from the blueprint)
qm create 9000 --name debian-12-tpl --memory 2048 --cores 2 \
  --cpu host --net0 virtio,bridge=vmbr0 --scsihw virtio-scsi-pci

# 3. import the cloud image as its boot disk
qm set 9000 --scsi0 local-lvm:0,import-from=/root/debian-12-generic-amd64.qcow2

# 4. attach the cloud-init config drive
qm set 9000 --ide2 local-lvm:cloudinit

# 5. boot from the imported disk; serial console (cloud images expect one)
qm set 9000 --boot order=scsi0
qm set 9000 --serial0 socket --vga serial0

# 6. freeze it — templates can't run, only be cloned
qm template 9000
```

Walk through what happened: steps 2–5 are the knobs you'd click in the UI
(note `bridge=vmbr0` — the virtual switch from the install lesson, and
`--cpu host` to pass through the real CPU's features, which Kubernetes
components appreciate). Step 6 marks it immutable. The template never
boots; it exists to be copied.

## Clone it with Terraform

Now the payoff. In `infra/main.tf`, define the control-plane VM from your
blueprint — but treat it as a **dry run for the whole idea**; we'll
destroy and recreate it on purpose in a minute:

```hcl
resource "proxmox_virtual_environment_vm" "k8s_cp_1" {
  name      = "k8s-cp-1"
  node_name = "pve-main"   # your node name from the installer
  vm_id     = 200
  pool_id   = proxmox_virtual_environment_pool.k8s.pool_id

  clone {
    vm_id = 9000
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
    enabled = false # see the gotcha below — Ansible flips this later
  }

  initialization {
    ip_config {
      ipv4 {
        address = "192.168.1.60/24"
        gateway = "192.168.1.1"
      }
    }

    user_account {
      username = "ops"
      keys     = [trimspace(file("~/.ssh/id_ed25519.pub"))]
    }
  }
}
```

The `initialization` block *is* cloud-init: on first boot the clone gives
itself that static IP and creates an `ops` user holding your SSH public
key (with passwordless sudo — Proxmox's cloud-init config sets that up for
the user it creates). Apply, give it a minute to boot, then from your
workstation:

```bash
terraform apply              # from infra/, like every terraform command
ssh ops@192.168.1.60
```

You're on a VM that no human installed.

**The guest-agent gotcha** (worth the paragraph — everyone hits it):
`agent { enabled = true }` tells Terraform to wait for the **QEMU guest
agent**, a small in-VM helper that reports IPs and enables clean
shutdowns. But Debian's cloud image doesn't ship it, so with it enabled,
`terraform apply` hangs for minutes waiting for an agent that will never
answer. We keep it `false` for now; the next lesson installs the agent
with Ansible, after which you can flip this to `true` and re-apply.

## Now break it on purpose

The entire point of this architecture is that VMs are disposable. Prove
it to yourself while there's nothing of value on the machine:

```bash
terraform destroy -target=proxmox_virtual_environment_vm.k8s_cp_1
terraform apply
ssh-keygen -R 192.168.1.60   # the rebuilt VM has a new host key
ssh ops@192.168.1.60
```

Gone and back in about a minute, identical both times. *This* is why the
control plane can live safely next to Jellyfin: the blast radius of any
Kubernetes mistake is one `terraform apply`.

## Challenge: A VM No Human Installed {#template-checkpoint points=15}

A `debian-12-tpl` template on the node, and `k8s-cp-1` cloned from it by
Terraform with a static IP and your SSH key — destroyed and recreated at
least once to prove it's cattle, not a pet.

### Starter

```python
# Self-attested checkpoint — flip DONE once you've honestly done the work.

# I built the cloud-init template with qm, Terraform cloned k8s-cp-1 from
# it, I SSHed in with my key, and I destroyed + recreated it to prove
# the whole thing is reproducible.
DONE = False

# For your own record: template VMID, clone boot time, anything that bit you.
NOTES = """
"""
```

### Tests

```python
from solution import DONE


def test_checkpoint_claimed():
    assert DONE is True, "flip DONE to True once the VM survived a rebuild"
```

# Lesson: Ansible — Configuring the Machines Code Built {#ansible-baseline}

Terraform answers "what machines exist?"; it's the wrong tool for "what's
installed *on* them?". That's Ansible's job: it SSHes into hosts from an
inventory and pushes them toward a described state — packages present,
services running, files in place. Like Terraform it's **idempotent**: a
task that's already true does nothing, so you rerun playbooks freely.

Install it on your workstation (`pipx install ansible` or your package
manager). No agent goes on the VMs — Ansible only needs the SSH access
cloud-init already gave you.

## Inventory: the machines you manage

Create `ansible/inventory.ini`. Groups (in brackets) let one playbook
target many hosts; ours mirror the blueprint so the Kubernetes section can
target `k8s_control` and `k8s_workers` separately later:

```ini
[k8s_control]
k8s-cp-1 ansible_host=192.168.1.60

[k8s_workers]
# k8s-w-1 ansible_host=192.168.1.61   # uncomment as workers come online

[k8s:children]
k8s_control
k8s_workers

[k8s:vars]
ansible_user=ops
```

Smoke-test connectivity (run ansible commands from the repo root — the
paths point into `ansible/`):

```bash
ansible -i ansible/inventory.ini k8s -m ping
```

`ping` here isn't ICMP — it SSHes in and runs a module end-to-end.
`"pong"` means inventory, DNS/IPs, SSH keys, and Python on the guest all
work.

## The baseline playbook

Every VM cloned from the template should get the same baseline. Create
`ansible/baseline.yml` — and note the first task pays off last lesson's
debt by installing the QEMU guest agent:

```yaml
- name: Baseline for all cluster VMs
  hosts: k8s
  become: true
  tasks:
    - name: Install base packages
      ansible.builtin.apt:
        name:
          - qemu-guest-agent
          - curl
          - vim
        update_cache: true

    - name: Enable and start the guest agent
      ansible.builtin.systemd_service:
        name: qemu-guest-agent
        state: started
        enabled: true

    - name: Upgrade everything
      ansible.builtin.apt:
        upgrade: dist
```

`become: true` means "use sudo" — which works passwordless because
cloud-init set the `ops` user up that way. Run it, twice:

```bash
ansible-playbook -i ansible/inventory.ini ansible/baseline.yml
ansible-playbook -i ansible/inventory.ini ansible/baseline.yml
```

The first run reports `changed` tasks; the second should be all `ok` —
idempotency you can see. With the agent now running, go flip
`agent { enabled = true }` in `infra/main.tf` and `terraform apply`: from
here on Proxmox can see guest IPs and shut VMs down cleanly.

Your rebuild story is now two commands: `terraform apply` creates the
machine, `ansible-playbook` makes it yours.

## Ansible can also *create* VMs — should it?

For completeness: Ansible has a `community.general.proxmox_kvm` module
that talks to the same API and can clone the template itself, no
Terraform involved:

```yaml
- name: Clone a scratch VM from the template
  community.general.proxmox_kvm:
    api_host: 192.168.1.10
    api_user: terraform@pve
    api_token_id: tf
    api_token_secret: "{{ lookup('env', 'PROXMOX_TOKEN_SECRET') }}"
    node: pve-main
    clone: debian-12-tpl
    name: scratch-vm
    storage: local-lvm
```

It works, and for a one-off scratch VM it's fine. But pick **one owner**
for VM lifecycle, or the tools fight: Terraform's state doesn't know about
VMs Ansible created, and its plans will never clean them up. This course's
split — **Terraform owns what exists, Ansible owns what's installed** — is
the common one in industry, and it's what the Kubernetes section builds
on. (When the VM fleet grows, look up Ansible's `community.general.proxmox`
*dynamic inventory* plugin, which builds the inventory by asking the
Proxmox API instead of maintaining `inventory.ini` by hand.)

## Challenge: Two Commands From Bare Metal {#ansible-checkpoint points=15}

An inventory and baseline playbook in the repo; the playbook run twice
(second run all `ok`); guest agent installed and `agent { enabled = true }`
applied in Terraform.

### Starter

```python
# Self-attested checkpoint — flip DONE once you've honestly done the work.

# ansible ping works against k8s-cp-1, baseline.yml ran clean twice, and
# Terraform now runs with the guest agent enabled.
DONE = False

# For your own record: what you added to the baseline beyond the course's.
NOTES = """
"""
```

### Tests

```python
from solution import DONE


def test_checkpoint_claimed():
    assert DONE is True, "flip DONE to True once baseline.yml is idempotent"
```

# Lesson: Milestone — The Foundation Is Code {#foundation}

Before Kubernetes enters the picture, prove the foundation's promise end
to end: **your infrastructure is a git repo, and reality is disposable.**
Everything from here on builds on that being actually true, not
aspirationally true.

## Challenge: Rebuild From Nothing {#foundation-rebuild points=25}

Do a full cold rebuild of the control-plane VM, from nothing to
configured, using only the repo:

1. From `infra/`:
   `terraform destroy -target=proxmox_virtual_environment_vm.k8s_cp_1`
   — and confirm in the web UI that it's gone.
2. `terraform apply` — the VM comes back: right ID, right pool, right IP,
   your SSH key, no console interaction.
3. From the repo root:
   `ansible-playbook -i ansible/inventory.ini ansible/baseline.yml` — the
   fresh VM gets its baseline; run it again and see all `ok`.
4. Commit everything (except the gitignored secrets and state) and read
   the repo as a stranger would: `README.md` with the blueprint,
   `infra/main.tf`, `infra/variables.tf`, `ansible/inventory.ini`,
   `ansible/baseline.yml`. That repo now *is* your homelab.

If the second machine has arrived, this is also the moment to run the
install lesson on it (`pve-worker`, `192.168.1.11`) — but that's a bonus,
not a requirement; the next lesson starts by bringing it into the fold.

### Starter

```python
# Self-attested checkpoint — flip each flag once it's honestly true.

# Step 1+2: destroyed and re-applied k8s-cp-1 purely from the repo.
REBUILT_FROM_CODE = False

# Step 3: baseline.yml ran on the fresh VM; a second run was all "ok".
BASELINE_IDEMPOTENT = False

# Step 4: the repo is committed and self-explanatory (README + tf + ansible).
REPO_IS_THE_LAB = False

# For your own record: total wall-clock time of the full rebuild.
NOTES = """
"""
```

### Tests

```python
from solution import BASELINE_IDEMPOTENT, REBUILT_FROM_CODE, REPO_IS_THE_LAB


def test_rebuilt_from_code():
    assert REBUILT_FROM_CODE is True


def test_baseline_idempotent():
    assert BASELINE_IDEMPOTENT is True


def test_repo_is_the_lab():
    assert REPO_IS_THE_LAB is True
```

# Lesson: Kubernetes, the Twenty-Minute Version {#k8s-in-brief}

You already know Kubernetes' core idea, because Terraform taught it to
you: **declare the state you want, let a machine reconcile reality toward
it.** The difference is *when*. Terraform reconciles once, when you run
`apply`. Kubernetes runs the loop forever, at runtime: you declare "three
replicas of this container, always", and controllers watch reality around
the clock — a container crashes at 3 a.m., a replacement is running at
3:00:07. That standing reconcile loop is the entire product; everything
else is vocabulary.

## The vocabulary you actually need

A **cluster** is a set of machines split into two roles — exactly the
split your blueprint drew:

- The **control plane** (your `k8s-cp-1` VM) is the brain: the **API
  server** is the front door every tool and component talks through; a
  **datastore** holds the declared and observed state; the **scheduler**
  decides which machine each new workload lands on; and **controllers**
  run the reconcile loops.
- **Workers** (the VMs you're about to carve out of `pve-worker`) do the
  actual work. Each runs a **kubelet** — the agent that takes orders from
  the API server and drives the local **container runtime**.

And four objects cover most of daily life:

- A **Pod** is the smallest deployable unit — one or more containers that
  live and die together. You almost never create pods directly.
- A **Deployment** declares "N replicas of this pod spec" and manages
  rollouts when you change the spec. This is what you write.
- A **Service** gives a set of pods one stable virtual IP and DNS name,
  because pods are cattle and their IPs churn.
- An **Ingress** routes HTTP from outside the cluster to Services by
  hostname and path.

## What k3s actually is

k3s is not "Kubernetes lite" — it's certified, conformant Kubernetes with
opinionated packaging. Upstream ships the control plane as separate
components you assemble (the `kubeadm` path); k3s compiles them into
**one binary with one systemd service** and bundles the choices you'd
otherwise research for a week: containerd as runtime, flannel as the pod
network, CoreDNS, a service load balancer (servicelb) that works on a
plain LAN, the Traefik ingress controller, a local-path storage
provisioner, and — on a single-server setup — SQLite standing in for
etcd as the datastore.

For this homelab that packaging is all upside: the control plane idles in
a few hundred MB next to Jellyfin instead of two gigabytes, and every
`kubectl` command, manifest, and concept transfers unchanged to any other
Kubernetes. The trade is that the components are hidden inside the
binary, so you don't *see* etcd or the scheduler as separate moving
parts. When you want that view — say, for the CKA exam — take the
companion course that builds vanilla Kubernetes from parts with kubeadm;
this one optimizes for a cluster you'll actually live on.

## Challenge: Say It Back {#k8s-in-brief-checkpoint points=5}

Close this lesson and explain out loud (rubber-duck style, fittingly):
what the API server, scheduler, and kubelet each do; why you write
Deployments instead of Pods; and what a Service solves that DNS-to-a-pod
wouldn't. If you can't, reread — section 2 uses these words constantly.

### Starter

```python
# Self-attested checkpoint — flip DONE once you've honestly done the work.

# I can explain the control-plane components, kubelet, and the
# Pod/Deployment/Service/Ingress ladder without peeking at the lesson.
DONE = False

# For your own record: the piece that took longest to click.
NOTES = """
"""
```

### Tests

```python
from solution import DONE


def test_checkpoint_claimed():
    assert DONE is True, "flip DONE to True once you can say it back"
```

# Lesson: Carving Up the Worker Node {#worker-node}

Time to bring the second machine into the fold and split it into worker
VMs — using only patterns you already have. This lesson is section 1
compressed into an hour, run against `pve-worker`.

## Bootstrap the second node

Three reruns of earlier lessons, on machine 2:

1. **Install Proxmox VE** (the install lesson): hostname `pve-worker`,
   static IP `192.168.1.11`, no-subscription repos, updated.
2. **Mint automation credentials** (the API lesson): its own
   `terraform@pve` user and token — the two nodes are separate worlds,
   each with its own API. Store the second secret next to the first.
3. **Build the template** (the cloud-init lesson): same `qm` recipe,
   same VMID 9000 — IDs only need to be unique *per node*.

One thing we deliberately *don't* do: join the two machines into a
Proxmox cluster. Proxmox clustering wants quorum — a majority vote among
nodes — and with exactly two members, one going down freezes changes on
the survivor until you intervene. There are workarounds (a third "vote"
via QDevice), but two standalone nodes managed by the same Terraform repo
give you everything this course needs with none of that ceremony.

## One repo, two hypervisors

Terraform handles multiple endpoints with **provider aliases** — same
provider plugin, second configuration. Add to `infra/main.tf`:

```hcl
provider "proxmox" {
  alias     = "worker"
  endpoint  = "https://192.168.1.11:8006/"
  api_token = var.proxmox_worker_api_token
  insecure  = true

  ssh {
    agent    = true
    username = "root"
  }
}
```

(and the matching `variable "proxmox_worker_api_token"` in
`infra/variables.tf`, with the value in your gitignored tfvars). Resources
default to the unaliased provider; anything for machine 2 opts in with
`provider = proxmox.worker`.

## Workers as data, not copy-paste

You could paste the `k8s_cp_1` resource twice and edit IPs. Instead,
meet `for_each` — Terraform's way of stamping one resource block out of a
map, which turns "add a worker" into "add one line":

```hcl
locals {
  workers = {
    "k8s-w-1" = { vm_id = 210, ip = "192.168.1.61" }
    "k8s-w-2" = { vm_id = 211, ip = "192.168.1.62" }
  }
}

resource "proxmox_virtual_environment_vm" "worker" {
  for_each = local.workers
  provider = proxmox.worker

  name      = each.key
  node_name = "pve-worker"
  vm_id     = each.value.vm_id

  clone {
    vm_id = 9000
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
    enabled = false # baseline.yml installs the agent, then flip this
  }

  initialization {
    ip_config {
      ipv4 {
        address = "${each.value.ip}/24"
        gateway = "192.168.1.1"
      }
    }

    user_account {
      username = "ops"
      keys     = [trimspace(file("~/.ssh/id_ed25519.pub"))]
    }
  }
}
```

`terraform apply`, and machine 2 sprouts two identical workers. Then
close the loop with the tools you already have: uncomment the workers in
`ansible/inventory.ini`, add `k8s-w-2`, and run the baseline —

```ini
[k8s_workers]
k8s-w-1 ansible_host=192.168.1.61
k8s-w-2 ansible_host=192.168.1.62
```

```bash
ansible -i ansible/inventory.ini k8s -m ping        # three pongs
ansible-playbook -i ansible/inventory.ini ansible/baseline.yml
```

— and flip the workers' `agent { enabled = true }` and re-apply. Every
machine in the blueprint now exists, is reachable, and came from code.

## Challenge: The Fleet Is Assembled {#worker-node-checkpoint points=15}

Both hypervisors online, two worker VMs defined via `for_each` on the
aliased provider, and `ansible -m ping` returning three pongs.

### Starter

```python
# Self-attested checkpoint — flip DONE once you've honestly done the work.

# pve-worker is installed with its own API token and template; k8s-w-1
# and k8s-w-2 exist via for_each under the aliased provider; baseline.yml
# ran on all three VMs and ansible ping gets three pongs.
DONE = False

# For your own record: worker sizing you chose, anything that differed
# from the pve-main runbook.
NOTES = """
"""
```

### Tests

```python
from solution import DONE


def test_checkpoint_claimed():
    assert DONE is True, "flip DONE to True once all three VMs pong"
```

# Lesson: k3s Server — a Control Plane in One Binary {#k3s-server}

Installing a k3s server is famously a one-liner. We're still going to do
it through Ansible — not for ceremony, but because "how did I install
this again?" is exactly the question your repo exists to answer, and
because the same playbook pattern joins the workers in the next lesson.

## The server playbook

The official installer script inspects env vars to decide what to set up.
Create `ansible/k3s-server.yml`:

```yaml
- name: Install k3s server (control plane)
  hosts: k8s_control
  become: true
  tasks:
    - name: Download the k3s installer
      ansible.builtin.get_url:
        url: https://get.k3s.io
        dest: /root/k3s-install.sh
        mode: "0755"

    - name: Run the installer in server mode
      ansible.builtin.command: /root/k3s-install.sh
      args:
        creates: /etc/systemd/system/k3s.service

    - name: Fetch the kubeconfig to the workstation
      ansible.builtin.fetch:
        src: /etc/rancher/k3s/k3s.yaml
        dest: kubeconfig-homelab
        flat: true
```

The `creates:` guard is what makes a raw shell command idempotent: if the
service file already exists, Ansible skips the task instead of
reinstalling. Run it:

```bash
ansible-playbook -i ansible/inventory.ini ansible/k3s-server.yml
```

Sixty-odd seconds later there is a Kubernetes control plane running next
to your media server, in one systemd unit (`systemctl status k3s` on the
VM if you're curious what one binary's worth of cluster looks like).

## kubectl from your workstation

`kubectl` is the client for everything, and it reads credentials from a
**kubeconfig** file — which the playbook just fetched. Two fixes before
it works: the file points at `127.0.0.1` (true on the server, not from
your desk), and it contains a client certificate that is **root on your
cluster**, so it gets secret-handling:

```bash
sed -i 's/127.0.0.1/192.168.1.60/' kubeconfig-homelab
chmod 600 kubeconfig-homelab
echo kubeconfig-homelab >> .gitignore     # it's a credential, like tfvars
export KUBECONFIG=$PWD/kubeconfig-homelab # add to your shell rc
```

Install kubectl on the workstation (your package manager has it), then
take your first look around:

```bash
kubectl get nodes
kubectl get pods --all-namespaces
```

One node, `Ready`, with roles `control-plane,master`. The pod list is the
k3s packaging made visible: `coredns` (cluster DNS), `traefik` (the
bundled ingress controller), `svclb-traefik` (servicelb doing
LoadBalancer duty on your LAN), `local-path-provisioner` (storage), and
`metrics-server`. You installed none of them individually — that's the
trade you chose in the mental-model lesson, working in your favor.

## Challenge: A Cluster of One {#k3s-server-checkpoint points=15}

k3s server installed by playbook, kubeconfig fetched, fixed, secured and
gitignored, and `kubectl get nodes` answering from your workstation.

### Starter

```python
# Self-attested checkpoint — flip DONE once you've honestly done the work.

# k3s-server.yml ran (and is idempotent on rerun), kubectl works from my
# workstation against 192.168.1.60, and the kubeconfig is chmod 600 and
# gitignored.
DONE = False

# For your own record: what `kubectl get pods -A` showed; anything Pending.
NOTES = """
"""
```

### Tests

```python
from solution import DONE


def test_checkpoint_claimed():
    assert DONE is True, "flip DONE to True once kubectl sees the node"
```

# Lesson: Workers Join the Cluster {#k3s-agents}

A machine joins a k3s cluster with exactly two facts: **where** the
server is, and a **join token** proving it's allowed in. The token was
generated at server install and sits on the control plane at
`/var/lib/rancher/k3s/server/node-token`. Guard it like a password — with
it, any machine on your LAN can enroll itself into your cluster.

## Reading the token where it lives

Don't copy the token into a file by hand — that's a secret in git or a
sticky note, and it goes stale if you ever rebuild the server. Instead,
have Ansible read it off the control plane *during the play* and hand it
to the workers. This is a two-play playbook, and it introduces
`hostvars` — how one host's facts get used while configuring another.
Create `ansible/k3s-agents.yml`:

```yaml
- name: Read the join token off the control plane
  hosts: k8s_control
  become: true
  tasks:
    - name: Slurp the node token
      ansible.builtin.slurp:
        src: /var/lib/rancher/k3s/server/node-token
      register: node_token

- name: Join the workers as k3s agents
  hosts: k8s_workers
  become: true
  vars:
    k3s_url: "https://192.168.1.60:6443"
    k3s_token: "{{ hostvars['k8s-cp-1'].node_token.content | b64decode | trim }}"
  tasks:
    - name: Download the k3s installer
      ansible.builtin.get_url:
        url: https://get.k3s.io
        dest: /root/k3s-install.sh
        mode: "0755"

    - name: Run the installer in agent mode
      ansible.builtin.command: /root/k3s-install.sh
      environment:
        K3S_URL: "{{ k3s_url }}"
        K3S_TOKEN: "{{ k3s_token }}"
      args:
        creates: /etc/systemd/system/k3s-agent.service
```

Same installer script; the presence of `K3S_URL` is what flips it to
agent mode. `slurp` returns file contents base64-encoded (hence the
`b64decode`), and `hostvars['k8s-cp-1']` reaches into facts registered by
the first play. Run it, then watch from your workstation:

```bash
ansible-playbook -i ansible/inventory.ini ansible/k3s-agents.yml
kubectl get nodes --watch
```

Within a minute or two: three nodes, all `Ready`. VMs on two different
physical machines, one cluster — the blueprint, realized.

## Keep the day job off the control plane

By default k3s schedules workloads on *every* node, server included. Your
server shares silicon with Jellyfin, and the blueprint says workers work,
the control plane thinks. Enforce that with a **taint** — a node-level
"keep out" that pods can only cross with a matching toleration:

```bash
kubectl taint node k8s-cp-1 node-role.kubernetes.io/control-plane=:NoSchedule
```

`NoSchedule` bars *new* pods but doesn't evict running ones, so the
system pods already on the server stay put (the bundled components
tolerate control-plane taints anyway — inspect one with
`kubectl -n kube-system get pod -o yaml` and find its `tolerations`).
From now on, everything you deploy lands on machine 2. While you're
labeling things, give the workers their role badge (cosmetic, but makes
`get nodes` output honest):

```bash
kubectl label node k8s-w-1 k8s-w-2 node-role.kubernetes.io/worker=true
```

## Challenge: Three Nodes, One Cluster {#k3s-agents-checkpoint points=15}

Both workers joined by playbook (token slurped, never written down), all
three nodes `Ready`, and the control plane tainted so workloads land on
`pve-worker`'s VMs only.

### Starter

```python
# Self-attested checkpoint — flip DONE once you've honestly done the work.

# k3s-agents.yml joined both workers using the slurped token, kubectl
# shows three Ready nodes, and k8s-cp-1 carries the NoSchedule taint.
DONE = False

# For your own record: join time per worker, output of `kubectl get nodes`.
NOTES = """
"""
```

### Tests

```python
from solution import DONE


def test_checkpoint_claimed():
    assert DONE is True, "flip DONE to True once three nodes are Ready"
```

# Lesson: First Workload — Deployment, Service, Ingress {#first-workload}

The cluster exists; make it earn its electricity. We'll deploy `whoami` —
a tiny web server that echoes back which pod served you, which makes
replicas and self-healing *visible* — and route real HTTP to it from your
LAN through the bundled Traefik ingress.

Manifests are declarations, exactly like `.tf` files, so they live in the
repo — in the `manifests/` directory that's been waiting empty since the
first lesson. Create `manifests/whoami.yaml`, all three rungs of the
ladder in one file:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: whoami
spec:
  replicas: 2
  selector:
    matchLabels:
      app: whoami
  template:
    metadata:
      labels:
        app: whoami
    spec:
      containers:
        - name: whoami
          image: traefik/whoami
          ports:
            - containerPort: 80
---
apiVersion: v1
kind: Service
metadata:
  name: whoami
spec:
  selector:
    app: whoami
  ports:
    - port: 80
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: whoami
spec:
  rules:
    - host: whoami.home.lan
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: whoami
                port:
                  number: 80
```

Read it bottom-up and it's a routing chain: Ingress says "HTTP for
`whoami.home.lan` goes to the `whoami` Service"; the Service says "I load
balance across pods labeled `app: whoami`"; the Deployment keeps two such
pods alive. Apply it and watch the scheduler respect your taint:

```bash
kubectl apply -f manifests/whoami.yaml
kubectl get pods -o wide     # both replicas on k8s-w-1 / k8s-w-2
```

## Reaching it from the couch

Traefik listens on a `LoadBalancer` Service, and k3s's servicelb answers
for it on your nodes' LAN IPs — check `kubectl -n kube-system get svc
traefik` and you'll see your actual `192.168.1.x` addresses under
`EXTERNAL-IP`. The only missing piece is DNS for the hostname; the
homelab-grade fix is an `/etc/hosts` line on your workstation (or a
proper record if you run a local DNS/Pi-hole):

```bash
echo "192.168.1.60 whoami.home.lan" | sudo tee -a /etc/hosts
curl http://whoami.home.lan
```

The response includes a `Hostname:` line naming the pod that answered —
curl a few times and watch it alternate between replicas. Then make the
reconcile loop show itself:

```bash
kubectl delete pod -l app=whoami --wait=false && kubectl get pods --watch
kubectl scale deployment whoami --replicas=5 && kubectl get pods -o wide
```

Murder both pods; the Deployment controller has replacements running in
seconds, because you declared *two replicas, always* and the loop never
sleeps. Scale to five with one command, back to two with another. Commit
`manifests/` — from here on, apps enter your cluster the same way VMs
enter your hypervisor: through the repo. (When the manifest pile grows,
the graduation path is GitOps — Flux or Argo CD applying the repo *for*
you — but that's a later course's rabbit hole.)

## Challenge: Served From Both Machines {#first-workload-checkpoint points=15}

`whoami` deployed from a committed manifest, reachable by hostname from
your workstation, alternating between replicas, and self-healing
demonstrated with your own eyes.

### Starter

```python
# Self-attested checkpoint — flip DONE once you've honestly done the work.

# whoami runs 2 replicas on the workers, curl whoami.home.lan works from
# my workstation and alternates pods, and I watched deleted pods get
# replaced by the Deployment controller.
DONE = False

# For your own record: EXTERNAL-IPs servicelb claimed, pod names you saw.
NOTES = """
"""
```

### Tests

```python
from solution import DONE


def test_checkpoint_claimed():
    assert DONE is True, "flip DONE to True once whoami answers by hostname"
```

# Final Challenge: Kill a Node, Keep Serving {#cluster-up points=50}

The whole stack is now code: Terraform makes VMs, Ansible makes them
cluster members, manifests make them serve. The final exam is proving the
stack absorbs the worst thing your homelab will actually experience — a
machine dying — and that you can regrow the lost limb from the repo.

With `whoami` running and reachable:

1. **Kill a worker for real.** From `infra/`: `terraform destroy
   -target='proxmox_virtual_environment_vm.worker["k8s-w-1"]'`. No
   drain, no warning — this is the power-supply-dies scenario.
2. **Watch Kubernetes cope.** `kubectl get nodes --watch` shows the node
   go `NotReady`; within a few minutes its pods are evicted and
   rescheduled onto the surviving worker (`kubectl get pods -o wide`).
   Keep curling `whoami.home.lan` throughout — brief blips are fair, but
   the app comes back without you touching anything.
3. **Regrow the limb.** `terraform apply` (in `infra/`) rebuilds the VM;
   `ansible-playbook -i ansible/inventory.ini ansible/baseline.yml` then
   `-i ansible/inventory.ini ansible/k3s-agents.yml` re-baselines and
   rejoins it. The
   rebuilt machine reuses the hostname `k8s-w-1`; if the old node object
   lingers as `NotReady`, clear it with `kubectl delete node k8s-w-1`
   *before* the rejoin and watch the fresh one register.
4. **Rebalance and verify.** Scale `whoami` up or delete its pods so
   replicas spread across both workers again. Three `Ready` nodes, app
   serving, `git status` clean except for your notes.

When this works you have earned an unusual sentence: *every layer of my
Kubernetes cluster, from hypervisor to HTTP response, is reproducible
from one git repository.* That's the foundation the container-native
world builds on — and yours runs next to the family movie library.

### Starter

```python
# Self-attested checkpoint — flip each flag once it's honestly true.

# Step 1+2: destroyed k8s-w-1 with Terraform; pods rescheduled and the
# app answered from the surviving worker without manual intervention.
APP_SURVIVED_NODE_LOSS = False

# Step 3: rebuilt the VM from the repo and it rejoined as a Ready node.
WORKER_REBUILT_AND_REJOINED = False

# Step 4: cluster back to full strength; repo alone describes everything.
CLUSTER_RESTORED_FROM_REPO = False

# For your own record: how long the app was unreachable, eviction timing.
NOTES = """
"""
```

### Tests

```python
from solution import (
    APP_SURVIVED_NODE_LOSS,
    CLUSTER_RESTORED_FROM_REPO,
    WORKER_REBUILT_AND_REJOINED,
)


def test_app_survived_node_loss():
    assert APP_SURVIVED_NODE_LOSS is True


def test_worker_rebuilt_and_rejoined():
    assert WORKER_REBUILT_AND_REJOINED is True


def test_cluster_restored_from_repo():
    assert CLUSTER_RESTORED_FROM_REPO is True
```
