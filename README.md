# Terraform Provider for Latitude.sh

[![GitHub release](https://img.shields.io/github/tag/latitudesh/terraform-provider-latitudesh.svg?label=release)](https://github.com/latitudesh/terraform-provider-latitudesh/releases/latest)
[![Go Reference](https://pkg.go.dev/badge/github.com/latitudesh/terraform-provider-latitudesh.svg)](https://pkg.go.dev/github.com/latitudesh/terraform-provider-latitudesh)
[![License: MPL 2.0](https://img.shields.io/badge/License-MPL_2.0-brightgreen.svg)](https://opensource.org/license/mpl-2-0/)

Provision and manage [Latitude.sh](https://www.latitude.sh/) bare metal infrastructure using [Terraform](https://developer.hashicorp.com/terraform).

Full documentation is available on the [Terraform Registry](https://registry.terraform.io/providers/latitudesh/latitudesh/latest/docs).

## Getting Started

Check the [latest releases](https://github.com/latitudesh/terraform-provider-latitudesh/releases/latest) for updates and changelogs.

> **Note:** If you are upgrading from version 1.x to 2.x, please see the [Migration Guide](MIGRATION_GUIDE_v2.md).

### Installation & Requirements

To get started, make sure you have:

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.6
- A [Latitude.sh API key](https://www.latitude.sh/dashboard/api-keys)

Then add the provider to your `terraform` block:

```hcl
terraform {
  required_providers {
    latitudesh = {
      source  = "latitudesh/latitudesh"
      version = ">= 2.5.0"
    }
  }
}
```

### Authentication

Export your API key:

```sh
export LATITUDESH_AUTH_TOKEN="<your-api-key-here>"
```

## Quick Example

```hcl
provider "latitudesh" {}

resource "latitudesh_server" "example" {
  billing           = "monthly"
  hostname          = "my-server"
  plan              = "c2-small-x86"
  site              = "SAO2"
  operating_system  = "ubuntu_24_04_x64_lts"
  project           = "proj_..."
  ssh_keys          = ["ssh_..."]
}
```

Then run:

```sh
terraform init
terraform apply
```

and type `yes` to confirm.

## Resources

Highlighted resources:

- [latitudesh_server](https://registry.terraform.io/providers/latitudesh/latitudesh/latest/docs/resources/server)
- [latitudesh_ssh_key](https://registry.terraform.io/providers/latitudesh/latitudesh/latest/docs/resources/ssh_key)
- [latitudesh_firewall](https://registry.terraform.io/providers/latitudesh/latitudesh/latest/docs/resources/firewall)
- [latitudesh_virtual_network](https://registry.terraform.io/providers/latitudesh/latitudesh/latest/docs/resources/virtual_network)

For the complete list of resources and data sources, see the [Terraform Registry documentation](https://registry.terraform.io/providers/latitudesh/latitudesh/latest/docs).

## Contributing

We welcome all contributions, from small fixes to major improvements. See [CONTRIBUTING.md](CONTRIBUTING.md) for more details.
