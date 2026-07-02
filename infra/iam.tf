resource "google_service_account" "app" {
  account_id   = "gc-app"
  display_name = "Get Cracked app (Cloud Run service)"

  depends_on = [google_project_service.apis["iam.googleapis.com"]]
}

# The grader SA deliberately has zero roles: signed URLs handed to each job
# execution are its only capability.
resource "google_service_account" "grader" {
  account_id   = "gc-grader"
  display_name = "Get Cracked grading jobs"

  depends_on = [google_project_service.apis["iam.googleapis.com"]]
}

resource "google_project_iam_member" "app_cloudsql" {
  project = var.project_id
  role    = "roles/cloudsql.client"
  member  = "serviceAccount:${google_service_account.app.email}"
}

resource "google_storage_bucket_iam_member" "app_grading_bucket" {
  bucket = google_storage_bucket.grading.name
  role   = "roles/storage.objectAdmin"
  member = "serviceAccount:${google_service_account.app.email}"
}

# Waiting on a RunJob LRO needs run.operations.get, which lives at project
# scope — job-scoped roles can't see operations.
resource "google_project_iam_member" "app_run_viewer" {
  project = var.project_id
  role    = "roles/run.viewer"
  member  = "serviceAccount:${google_service_account.app.email}"
}

# run.jobs.runWithOverrides (per-execution env) needs more than run.invoker;
# run.developer scoped to each job is the established role that carries it.
resource "google_cloud_run_v2_job_iam_member" "app_runs_jobs" {
  for_each = google_cloud_run_v2_job.graders

  name     = each.value.name
  location = var.region
  role     = "roles/run.developer"
  member   = "serviceAccount:${google_service_account.app.email}"
}

# Keyless V4 URL signing: the storage client calls the IAM Credentials
# SignBlob API as the app SA, which requires tokenCreator on itself.
resource "google_service_account_iam_member" "app_self_signer" {
  service_account_id = google_service_account.app.name
  role               = "roles/iam.serviceAccountTokenCreator"
  member             = "serviceAccount:${google_service_account.app.email}"
}
