terraform {
  required_version = ">= 1.6"

  # Remote state so CD (GitHub Actions) and local `tofu` share one source
  # of truth. Bucket is created out-of-band (see README "CI/CD setup");
  # migrate existing local state into it with `tofu init -migrate-state`.
  backend "gcs" {
    bucket = "getcracked-touch-grass-tfstate"
    prefix = "terraform/state"
  }

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }
}
