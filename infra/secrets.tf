resource "google_secret_manager_secret" "database_url" {
  secret_id = "gc-database-url"

  replication {
    auto {}
  }

  depends_on = [google_project_service.apis["secretmanager.googleapis.com"]]
}

resource "google_secret_manager_secret_version" "database_url" {
  secret = google_secret_manager_secret.database_url.id
  # pgx unix-socket form; no TLS needed over the Cloud SQL socket mount.
  secret_data = "postgres://${google_sql_user.app.name}:${random_password.db.result}@/${google_sql_database.app.name}?host=/cloudsql/${google_sql_database_instance.postgres.connection_name}"
}

resource "google_secret_manager_secret_iam_member" "app_reads_database_url" {
  secret_id = google_secret_manager_secret.database_url.id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.app.email}"
}
