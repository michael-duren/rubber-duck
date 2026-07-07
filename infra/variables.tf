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

variable "domain" {
  description = "Apex domain for the app; apex + www are both mapped to Cloud Run"
  type        = string
  default     = "duckgc.com"
}
