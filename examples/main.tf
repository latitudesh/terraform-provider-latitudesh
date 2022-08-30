terraform {
  required_providers {
    latitude = {
      version = "0.0.1"
      source  = "capturealpha.com/iac/latitude"
    }
  }
}

provider "latitude" {
  auth_token = "9466f6dcdddaee89d00b6584cf31c0d66976"
}