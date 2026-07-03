resource "google_cloud_run_v2_service" "app" {
  name     = "gc-app"
  location = var.region
  ingress  = "INGRESS_TRAFFIC_ALL"

  template {
    service_account = google_service_account.app.email

    scaling {
      min_instance_count = 0
      max_instance_count = 3
    }

    volumes {
      name = "cloudsql"
      cloud_sql_instance {
        instances = [google_sql_database_instance.postgres.connection_name]
      }
    }

    containers {
      image = "${local.registry}/getcracked:${var.image_tag}"

      env {
        name  = "GC_GRADER"
        value = "cloudrun"
      }
      env {
        name  = "GC_PROJECT"
        value = var.project_id
      }
      env {
        name  = "GC_REGION"
        value = var.region
      }
      env {
        name  = "GC_GRADING_BUCKET"
        value = google_storage_bucket.grading.name
      }
      env {
        name = "DATABASE_URL"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.database_url.secret_id
            version = "latest"
          }
        }
      }

      volume_mounts {
        name       = "cloudsql"
        mount_path = "/cloudsql"
      }

      resources {
        limits = {
          cpu    = "1"
          memory = "512Mi"
        }
        # Grading submissions wait on a background goroutine (the pool
        # worker polling a Cloud Run Job) that isn't tied to any one HTTP
        # request. With the default cpu_idle=true, Cloud Run throttles CPU
        # to near-zero between requests, so that goroutine barely runs and
        # a submission can sit at "running" for minutes even after its job
        # execution finished — always-allocate CPU so grading keeps making
        # progress in the background.
        cpu_idle = false
      }
    }
  }

  depends_on = [
    google_project_service.apis["run.googleapis.com"],
    google_secret_manager_secret_iam_member.app_reads_database_url,
    google_secret_manager_secret_version.database_url,
  ]
}

resource "google_cloud_run_v2_service_iam_member" "public" {
  name     = google_cloud_run_v2_service.app.name
  location = var.region
  role     = "roles/run.invoker"
  member   = "allUsers"
}
