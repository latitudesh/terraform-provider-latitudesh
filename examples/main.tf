terraform {
  required_providers {
    latitude = {
      source  = "latitudesh/latitudesh"
      version = ">=0.1.2"
    }
  }
}

provider "latitude" {
  auth_token = var.auth_token
}
