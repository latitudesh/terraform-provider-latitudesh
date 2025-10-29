---
page_title: "latitudesh_tag Data Source - terraform-provider-latitudesh"
subcategory: ""
description: |-
  Lookup a tag by id, name, or slug.
---

# latitudesh_tag (Data Source)

Retrieve a [tag](https://www.latitude.sh/dashboard/tags) by a single field: `id`, `name`, or `slug`.

## Example Usage

### Lookup by ID

```hcl
data "latitudesh_tag" "by_id" {
  id = "tag_..."
}

output "tag_color" {
  value = data.latitudesh_tag.by_id.color
}
```

### Lookup by name

```hcl
data "latitudesh_tag" "production" {
  name = "production"
}

output "tag_slug" {
  value = data.latitudesh_tag.production.slug
}
```

### Lookup by slug

```hcl
data "latitudesh_tag" "by_slug" {
  slug = "production-env"
}
```

### Use with a server resource

```hcl
data "latitudesh_tag" "production" {
  name = "production"
}

data "latitudesh_tag" "web_tier" {
  name = "web-tier"
}

variable "project" {
  type    = string
  default = "proj_..."
}

resource "latitudesh_server" "server" {
  billing          = "monthly"
  project          = var.project
  hostname         = "web-prd-01"
  plan             = "c2-small-x86"
  site             = "SAO2"
  operating_system = "ubuntu_22_04_x64_lts"

  # Add tag IDs to a server
  tags = [
    data.latitudesh_tag.production.id,
    data.latitudesh_tag.web_tier.id
  ]
}
```

### Using `for_each` with multiple tags

```hcl
variable "project" {
  type    = string
  default = "proj_..."
}

variable "server_count" {
  type    = number
  default = 3
}

variable "tag_names" {
  type    = set(string)
  default = ["production", "web-tier", "critical"]
}

data "latitudesh_tag" "tags" {
  for_each = var.tag_names
  name     = each.value
}

locals {
  tag_ids = [for tag in data.latitudesh_tag.tags : tag.id]
}

resource "latitudesh_server" "server" {
  count            = var.server_count
  project          = var.project
  operating_system = "ubuntu_22_04_x64_lts"
  plan             = "c2-small-x86"
  billing          = "monthly"
  site             = "SAO2"

  # prd-web-01, prd-web-02, prd-web-03, ...
  hostname         = format("prd-web-%02d", count.index + 1)

  tags             = local.tag_ids
}

output "tag_ids" {
  value = local.tag_ids
}
```

### Conditional tag assignment

```hcl
variable "environment" {
  type    = string
  default = "production"
}

data "latitudesh_tag" "env_tag" {
  name = var.environment
}

resource "latitudesh_server" "server" {
  project          = "proj_..."
  hostname         = "app-server"
  plan             = "c2-small-x86"
  site             = "ASH"
  operating_system = "ubuntu_22_04_x64_lts"

  tags = [data.latitudesh_tag.env_tag.id]
}

output "environment_color" {
  value       = data.latitudesh_tag.env_tag.color
  description = "The color code associated with the environment tag"
}
```


## Schema

### Argument Reference

- `id` (Optional) The tag ID to look up. Exactly one of id, name, or slug must be set
- `name` (Optional) The tag name to look up. Should be unique within your team
- `slug` (Optional) The tag slug to look up. Slugs are automatically generated from tag names

### Read-Only

In addition to the arguments above, the following attributes are exported:

- `id` (String) The tag ID
- `name` (String) The tag name
- `slug` (String) The tag slug (URL-friendly version of the name)
- `description` (String) The tag description (may be null if not set)
- `color` (String) The tag color as a hexadecimal color code (e.g., #ff0000)
