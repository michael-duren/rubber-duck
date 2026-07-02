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
