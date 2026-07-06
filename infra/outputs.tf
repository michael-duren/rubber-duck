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

output "domain_dns_records" {
  description = "DNS records to enter at the registrar (apex A/AAAA, www CNAME)"
  value = {
    for domain, mapping in google_cloud_run_domain_mapping.app :
    domain => try(mapping.status[0].resource_records, [])
  }
}
