---
page_title: "latitudesh_ssh_key Data Source - terraform-provider-latitudesh"
subcategory: ""
description: |-
  Lookup an SSH key by id, name, or fingerprint.
---

# latitudesh_ssh_key (Data Source)

Retrieve an [SSH key](https://www.latitude.sh/dashboard/ssh-keys) by a single field: `id`, `name`, or `fingerprint`.

## Example Usage

### Lookup by ID

```hcl
data "latitudesh_ssh_key" "by_id" {
  id = "ssh_..."
}

output "ssh_key_pub" {
  value = data.latitudesh_ssh_key.by_id.public_key
}
```

### Lookup by name

```hcl
data "latitudesh_ssh_key" "by_name" {
  name = "deploy-bot"
}
```

### Lookup by fingerprint

```hcl
data "latitudesh_ssh_key" "by_fp" {
  fingerprint = "SHA256:AbCdEf123..."
}
```

### Use with a resource

```hcl
data "latitudesh_ssh_key" "deploy" {
  name = "deploy-bot"
}

variable "project" {
  type    = string
  default = "proj_..."
}

resource "latitudesh_server" "server" {
  billing  = "monthly"
  project  = var.project
  hostname = "prd-01"
  plan     = "c2-small-x86"
  site     = "SAO2"

  # Add SSH key IDs to a server
  ssh_keys = [data.latitudesh_ssh_key.deploy.id]
}
```

### Using `for_each`

```hcl
variable "project" {
  type    = string
  default = "proj_..."
}

variable "server_count" {
  type    = number
  default = 3
}

variable "ssh_key_names" {
  type    = set(string)
  default = ["ci", "dev-laptop-01"]
}

data "latitudesh_ssh_key" "keys" {
  for_each = var.ssh_key_names
  name     = each.value
}

locals {
  ssh_key_ids = [for key in data.latitudesh_ssh_key.keys : key.id]
}

resource "latitudesh_server" "server" {
  count            = var.server_count
  project          = var.project
  operating_system = "ubuntu_22_04_x64_lts"
  plan             = "c2-small-x86"
  billing          = "monthly"
  site             = "SAO2"

  # prd-01, prd-02, prd-03, ...
  hostname         = format("prd-%02d", count.index + 1)

  ssh_keys         = local.ssh_key_ids
}

output "ssh_key_ids" {
  value = local.ssh_key_ids
}
```


## Schema

### Argument Reference

- `id` (Optional) The SSH key ID to look up. Exactly one of id, name, or fingerprint must be set
- `name` (Optional) The SSH key name to look up. Should be unique within your account
- `fingerprint` (Optional) The SSH key fingerprint to look up

### Read-Only

In addition to the arguments above, the following attributes are exported:

- `id` (String) The SSH key ID
- `name` (String) The SSH key name
- `public_key` (String) The SSH public key
- `fingerprint` (String) The SSH key fingerprint
- `created_at` (String) Creation timestamp
- `updated_at` (String) Last update timestamp
