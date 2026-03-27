terraform {
  required_providers {
    scaleway = {
      source  = "scaleway/scaleway"
      version = "~> 2.0"
    }
  }
  required_version = ">= 1.0"
}

provider "scaleway" {
  region = "fr-par"
  zone   = "fr-par-1"
}
