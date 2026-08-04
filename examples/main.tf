terraform {
  required_providers {
    latitudesh = {
      source = "latitudesh/latitudesh"
    }
  }
}

# Configure the provider
provider "latitudesh" {
  auth_token = var.latitudesh_token
}
