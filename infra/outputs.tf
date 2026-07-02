output "service_url" {
  description = "Public URL of the app"
  value       = google_cloud_run_v2_service.app.uri
}

output "registry_url" {
  description = "Artifact Registry prefix for docker push"
  value       = local.registry
}

output "sql_connection_name" {
  description = "Cloud SQL connection name (for cloud-sql-proxy)"
  value       = google_sql_database_instance.postgres.connection_name
}

output "db_password" {
  description = "Database password (for local cloud-sql-proxy seeding)"
  value       = random_password.db.result
  sensitive   = true
}
