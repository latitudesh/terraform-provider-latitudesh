terraform {
  required_providers {
    latitude = {
      source  = "latitudesh/latitudesh"
      version = "~> 0.1.1"
    }
  }
}

# Configure the provider
provider "latitude" {
  auth_token = var.latitude_token
}
