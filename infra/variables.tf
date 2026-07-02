variable "project_id" {
  description = "GCP project ID (must already exist with billing enabled)"
  type        = string
}

variable "region" {
  description = "Region for all resources"
  type        = string
  default     = "us-central1"
}

variable "image_tag" {
  description = "Image tag for the app and runner images; use a unique tag per deploy"
  type        = string
  default     = "v1"
}
