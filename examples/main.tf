terraform {
  required_providers {
    latitude = {
      version = "0.0.1"
      source  = "capturealpha.com/iac/latitude"
    }
  }
}

provider "latitude" {
  auth_token = var.auth_token
}