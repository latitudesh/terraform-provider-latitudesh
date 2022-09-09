terraform {
  required_providers {
    latitude = {
      version = "0.0.6-pre"
      source  = "capturealpha.com/iac/latitude"
    }
  }
}

provider "latitude" {
  auth_token = var.auth_token
}