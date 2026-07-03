# Restricts grading job network egress to Google APIs only (specifically
# GCS, for the signed-URL upload/download protocol), so submission code
# can't exfiltrate data or reach arbitrary hosts. gVisor plus a zero-role
# service account limit blast radius; this closes the remaining "open
# internet" gap documented at M2.
#
# Pattern: a dedicated VPC + Serverless VPC Access connector, jobs routed
# through it with egress=ALL_TRAFFIC, a private DNS zone overriding
# *.googleapis.com to the restricted.googleapis.com VIP (a fixed /30 that
# only serves a curated set of GA Google APIs — GCS included), and firewall
# rules allowing egress only to that VIP (deny-all everything else).
#
# NOT applied by this commit — see the review note in
# issues/for-human/ before running `tofu apply` for this file. It touches
# the only production grading path and needs a live end-to-end check
# (curl-to-arbitrary-host fails, grading still passes) that's expensive to
# iterate on blind (each Cloud Run Job execution takes minutes).

resource "google_compute_network" "vpc" {
  name                    = "gc-vpc"
  auto_create_subnetworks = false
}

resource "google_compute_subnetwork" "connector" {
  name          = "gc-connector-subnet"
  network       = google_compute_network.vpc.id
  region        = var.region
  ip_cidr_range = "10.8.0.0/28" # minimum size a VPC Access connector accepts

  # Lets resources in this subnet reach Google APIs over their internal
  # (non-internet) path once DNS is overridden to restricted.googleapis.com.
  private_ip_google_access = true
}

resource "google_vpc_access_connector" "grader" {
  name   = "gc-grader-connector"
  region = var.region

  subnet {
    name = google_compute_subnetwork.connector.name
  }

  # Smallest footprint the connector supports; grading concurrency is low
  # (internal/grader/pool.go runs 2 workers) so this is not a bottleneck.
  machine_type  = "e2-micro"
  min_instances = 2
  max_instances = 3

  depends_on = [google_project_service.apis["vpcaccess.googleapis.com"]]
}

# --- Private DNS override: *.googleapis.com -> restricted.googleapis.com ---
# Without this, the job's `curl https://storage.googleapis.com/...` would
# resolve to Google's regular public IPs, which are NOT covered by the
# firewall rule below (they're not a small, stable range) — so the
# override is what makes "allow only restricted.googleapis.com" work in
# practice, not just on paper.

resource "google_dns_managed_zone" "restricted_googleapis" {
  name        = "gc-restricted-googleapis"
  dns_name    = "googleapis.com."
  description = "Overrides *.googleapis.com to the restricted VIP for grading jobs"
  visibility  = "private"

  private_visibility_config {
    networks {
      network_url = google_compute_network.vpc.id
    }
  }

  depends_on = [google_project_service.apis["dns.googleapis.com"]]
}

resource "google_dns_record_set" "restricted_googleapis_cname" {
  name         = "*.googleapis.com."
  type         = "CNAME"
  ttl          = 300
  managed_zone = google_dns_managed_zone.restricted_googleapis.name
  rrdatas      = ["restricted.googleapis.com."]
}

resource "google_dns_record_set" "restricted_googleapis_a" {
  name         = "restricted.googleapis.com."
  type         = "A"
  ttl          = 300
  managed_zone = google_dns_managed_zone.restricted_googleapis.name
  rrdatas      = ["199.36.153.4", "199.36.153.5", "199.36.153.6", "199.36.153.7"]
}

# --- Firewall: deny all egress except to the restricted VIP (and the
# connector's own subnet traffic, which Serverless VPC Access needs) ---

resource "google_compute_firewall" "allow_restricted_googleapis_egress" {
  name      = "gc-allow-restricted-googleapis-egress"
  network   = google_compute_network.vpc.id
  direction = "EGRESS"
  priority  = 1000

  destination_ranges = ["199.36.153.4/30"]
  allow {
    protocol = "tcp"
    ports    = ["443"]
  }
}

resource "google_compute_firewall" "allow_connector_internal" {
  name      = "gc-allow-connector-internal"
  network   = google_compute_network.vpc.id
  direction = "EGRESS"
  priority  = 1000

  destination_ranges = [google_compute_subnetwork.connector.ip_cidr_range]
  allow {
    protocol = "all"
  }
}

# Serverless VPC Access health-checks the connector's instances from this
# range; required for the connector to report healthy.
resource "google_compute_firewall" "allow_connector_health_check" {
  name      = "gc-allow-connector-health-check"
  network   = google_compute_network.vpc.id
  direction = "INGRESS"
  priority  = 1000

  source_ranges = ["35.199.224.0/19"]
  allow {
    protocol = "tcp"
    ports    = ["667"]
  }
}

resource "google_compute_firewall" "deny_all_egress" {
  name      = "gc-deny-all-egress"
  network   = google_compute_network.vpc.id
  direction = "EGRESS"
  priority  = 65534

  destination_ranges = ["0.0.0.0/0"]
  deny {
    protocol = "all"
  }
}
