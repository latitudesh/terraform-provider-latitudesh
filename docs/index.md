---
page_title: "Provider: Latitude.sh"
---

# Terraform Provider for Latitude.sh

The Latitude.sh provider allows you to interact with the resources supported by [Latitude.sh](https://www.latitude.sh). The provider must be configured with valid credentials before it can be used.

Use the navigation menu on the left to explore the available resources. Please note that all resources require authentication.

> **Upgrading to v2:** If you are migrating from version 1.x to 2.x, please see the [Migration Guide](https://github.com/latitudesh/terraform-provider-latitudesh/blob/main/MIGRATION_GUIDE_v2.md).

## Authentication

API keys can be created in the [Latitude.sh dashboard](https://www.latitude.sh/dashboard/api-keys).

The provider supports authentication via the environment variable `LATITUDESH_AUTH_TOKEN`, or explicitly using the `auth_token` argument in the provider configuration.

Export your API key:

```sh
export LATITUDESH_AUTH_TOKEN="<your-api-key-here>"
```

Or configure your provider with `auth_token`:

```hcl
provider "latitudesh" {
  auth_token = var.latitudesh_token
}
```

## Example Usage

#### `main.tf`

```hcl
terraform {
  required_providers {
    latitudesh = {
      source  = "latitudesh/latitudesh"
      version = ">= 2.5.0"
    }
  }
}

provider "latitudesh" {}
```

#### `variables.tf`

```hcl
variable "billing" {
  description = "The server billing type"
  default     = "monthly"
}

variable "hostname" {
  description = "The server hostname"
  default     = "terraform-latitudesh"
}

variable "operating_system" {
  description = "The server OS"
  default     = "ubuntu_24_04_x64_lts"
}

variable "plan" {
  description = "The server plan"
  default     = "c2-small-x86"
}

data "latitudesh_region" "region" {
  slug = "SAO2"
}
```

#### `resources.tf`

```hcl
resource "latitudesh_project" "new_project" {
  name              = "The project name must be unique"
  description       = "The project description"
  environment       = "Development" # Development, Production or Staging
  provisioning_type = "on_demand"   # on_demand or reserved
}

resource "latitudesh_ssh_key" "ssh_key" {
  name        = "Name of the SSH Key"
  public_key  = "ssh-ed25519 AAA...REPLACE_ME... user@example.com"
}
```

```hcl
# A project must be created before creating a server
resource "latitudesh_server" "server" {
  billing          = var.billing
  hostname         = var.hostname
  operating_system = var.operating_system
  plan             = var.plan
  project          = latitudesh_project.new_project.id  # You can use the project id or slug
  site             = data.latitudesh_region.region.slug # You can use the site id or slug
  ssh_keys         = [latitudesh_ssh_key.ssh_key.id]
}
```

## Importing existing resources

You can import existing Latitude.sh resources into your Terraform state using the `import` block. This is useful when you already have a resource deployed and you want to manage using Terraform.

Example of importing a server:

```hcl
terraform {
  required_providers {
    latitudesh = {
      source  = "latitudesh/latitudesh"
      version = ">= 2.5.0"
    }
  }
}

import {
  to = latitudesh_server.server
  id = "sv_your_server_id_here"
}
```

Then run:

```sh
terraform plan -generate-config-out=server.tf
```

This will create a `server.tf` file with the current configuration of your imported server, which you can then customize as needed.
