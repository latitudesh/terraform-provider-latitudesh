resource "latitudesh_project" "block_storage" {
  name              = "Block storage example"
  environment       = "Development"
  provisioning_type = "on_demand"
}

resource "latitudesh_block_storage" "example" {
  project    = latitudesh_project.block_storage.id
  name       = "app-data"
  region     = "ASH"
  size_in_gb = 100
}

data "latitudesh_block_storage" "by_id" {
  id = latitudesh_block_storage.example.id
}

data "latitudesh_block_storage" "by_name" {
  name    = "app-data"
  project = latitudesh_project.block_storage.id
}
