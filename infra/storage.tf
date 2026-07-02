resource "google_storage_bucket" "grading" {
  name                        = "gc-grading-${var.project_id}"
  location                    = var.region
  uniform_bucket_level_access = true
  force_destroy               = true

  # Grading artifacts are transient; the app deletes them per run and this
  # rule is the backstop.
  lifecycle_rule {
    condition {
      age = 1
    }
    action {
      type = "Delete"
    }
  }

  depends_on = [google_project_service.apis["storage.googleapis.com"]]
}
