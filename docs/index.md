---
page_title: "Provider: Latitude.sh"
---

# Latitude.sh Provider

The Latitude.sh provider is used to interact with the resources supported by [Latitude.sh](https://www.latitude.sh). The provider needs to be configured with the proper credentials before it can be used.

Use the navigation to the left to read about the available resources.

All resources require authentication. API keys can be obtained from your Latitude.sh dashboard.

## Example usage

```terraform
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
```

```terraform
resource "latitude_server" "server" {
  hostname = "foo"
  operating_system = "ubuntu_20_04_x64_lts"
  plan = data.latitude_plan.plan.slug
  project_id = latitude_project.project.id
  site = data.latitude_region.region.slug
  ssh_keys = [latitude_ssh_key.ssh_key.id]
}
```