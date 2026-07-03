locals {
  grader_languages = ["go", "python"]
}

resource "google_cloud_run_v2_job" "graders" {
  for_each = toset(local.grader_languages)

  name     = "gc-grader-${each.value}"
  location = var.region

  template {
    template {
      service_account = google_service_account.grader.email
      timeout         = "90s"
      max_retries     = 0

      # All egress through the connector, so the deny-all-except-Google-APIs
      # firewall in network.tf actually applies to submission code — without
      # ALL_TRAFFIC, only RFC1918-destined traffic would route through it.
      vpc_access {
        connector = google_vpc_access_connector.grader.id
        egress    = "ALL_TRAFFIC"
      }

      containers {
        image = "${local.registry}/gc-runner-${each.value}:${var.image_tag}"

        resources {
          limits = {
            cpu    = "1"
            memory = "512Mi"
          }
        }
      }
    }
  }

  depends_on = [google_project_service.apis["run.googleapis.com"]]
}
