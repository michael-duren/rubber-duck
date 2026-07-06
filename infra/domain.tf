# Custom-domain mappings for the app. Uses Cloud Run's built-in domain
# mapping (v1 domainmapping API) rather than an external load balancer:
# free, and enough for this traffic. If we ever need CDN/Cloud Armor or a
# static anycast IP, swap these for a serverless NEG behind a global ALB.
#
# PREREQUISITE (one-time, out of band — tofu can't do the verification
# handshake): prove domain ownership before apply, otherwise the create
# fails with a permission error.
#
#   gcloud domains verify duckgc.com
#
# That adds a TXT record at the registrar via Search Console; once the apex
# is verified, www inherits. After apply, read the `domain_dns_records`
# output and enter those A/AAAA (apex) and CNAME (www) records at Namecheap.
# Google provisions the managed TLS cert automatically once DNS resolves
# (typically 15-60 min).

locals {
  domains = toset([var.domain, "www.${var.domain}"])
}

resource "google_cloud_run_domain_mapping" "app" {
  for_each = local.domains

  name     = each.value
  location = var.region

  metadata {
    namespace = var.project_id
  }

  spec {
    route_name = google_cloud_run_v2_service.app.name
  }
}
