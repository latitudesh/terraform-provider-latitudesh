terraform {
  required_providers {
    latitude = {
      version = "0.0.6"
      source  = "latitude.sh/iac/latitudesh"
    }
  }
}

provider "latitude" {
  auth_token = var.auth_token
}