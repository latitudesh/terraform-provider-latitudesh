---
page_title: "Provider: Latitude.sh"
---

# Latitude.sh Provider

The Latitude.sh provider is used to interact with the resources supported by [Latitude.sh](https://www.latitude.sh). The provider needs to be configured with the proper credentials before it can be used.

Use the navigation to the left to read about the available resources.

All resources require authentication. API keys can be obtained from your Latitude.sh dashboard.

## Example usage

`main.tf` example

```terraform
terraform {
  required_providers {
    latitudesh = {
      source  = "latitudesh/latitudesh"
      version = "1.0.0-rc.2"
    }
  }
}

# Configure the provider
provider "latitudesh" {
  auth_token = var.latitudesh_token
}
```

`variables.tf` example

```terraform
variable "latitudesh_token" {
  description = "Latitude.sh API token"
}

variable "plan" {
  description = "Latitude.sh server plan"
  default     = "s3-large-x86"
}

variable "region" {
  description = "Latitude.sh server region slug"
  default     = "ASH"
}

variable "ssh_public_key" {
  description = "Latitude.sh SSH public key"
}
```

`latitudesh_server.tf` example

```terraform
resource "latitudesh_server" "server" {
  hostname         = "terraform.latitude.sh"
  operating_system = "ubuntu_22_04_x64_lts"
  plan             = data.latitudesh_plan.plan.slug
  project          = latitudesh_project.project.id      # You can use the project id or slug
  site             = data.latitudesh_region.region.slug # You can use the site id or slug
  ssh_keys         = [latitudesh_ssh_key.ssh_key.id]
}
```