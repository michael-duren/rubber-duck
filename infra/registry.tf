resource "google_artifact_registry_repository" "images" {
  repository_id = "getcracked"
  location      = var.region
  format        = "DOCKER"
  description   = "Get Cracked app and grading runner images"

  depends_on = [google_project_service.apis["artifactregistry.googleapis.com"]]
}

locals {
  registry = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.images.repository_id}"
}
