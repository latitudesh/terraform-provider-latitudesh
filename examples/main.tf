terraform {
  required_providers {
    latitudesh = {
      source  = "latitudesh/latitudesh"
      version = "1.0.0"
    }
  }
}

# Configure the provider
provider "latitudesh" {
  auth_token = var.latitudesh_token
}
