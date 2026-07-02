resource "google_sql_database_instance" "postgres" {
  name             = "gc-postgres"
  database_version = "POSTGRES_17"
  region           = var.region

  settings {
    edition = "ENTERPRISE"
    tier    = "db-f1-micro"

    ip_configuration {
      ipv4_enabled = true # public IP; access is via the Cloud SQL connector/proxy only
    }
  }

  deletion_protection = false

  depends_on = [google_project_service.apis["sqladmin.googleapis.com"]]
}

resource "google_sql_database" "app" {
  name     = "getcracked"
  instance = google_sql_database_instance.postgres.name
}

# The password lands in a URL: keep it alphanumeric.
resource "random_password" "db" {
  length  = 32
  special = false
}

resource "google_sql_user" "app" {
  name     = "getcracked"
  instance = google_sql_database_instance.postgres.name
  password = random_password.db.result
}
