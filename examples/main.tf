terraform {
  required_providers {
    latitudesh = {
      source  = "latitudesh/latitudesh"
      version = "~> 0.2.5"
    }
  }
}

# Configure the provider
provider "latitudesh" {
  auth_token = var.latitudesh_token
}
