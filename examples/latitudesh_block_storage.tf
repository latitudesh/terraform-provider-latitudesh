variable "project" {
  type    = string
  default = "proj_..."
}

resource "latitudesh_block_storage" "example" {
  project    = var.project
  name       = "example-volume"
  region     = "SAO" # site/region slug; not returned by the read API, see docs
  size_in_gb = 100
}

data "latitudesh_block_storage" "by_id" {
  id = latitudesh_block_storage.example.id
}

data "latitudesh_block_storage" "by_name" {
  project = var.project
  name    = latitudesh_block_storage.example.name
}

output "block_storage_id" {
  value = latitudesh_block_storage.example.id
}

output "block_storage_namespace_id" {
  value = latitudesh_block_storage.example.namespace_id
}
