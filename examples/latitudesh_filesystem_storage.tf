resource "latitudesh_project" "filesystem_storage" {
  name              = "Filesystem storage example"
  environment       = "Development"
  provisioning_type = "on_demand"
}

resource "latitudesh_filesystem_storage" "example" {
  project    = latitudesh_project.filesystem_storage.id
  name       = "shared-data"
  region     = "ASH"
  size_in_gb = 1500
  protocols  = ["nfs4"]
}

data "latitudesh_filesystem_storage" "by_id" {
  id = latitudesh_filesystem_storage.example.id
}

data "latitudesh_filesystem_storage" "by_name" {
  name    = "shared-data"
  project = latitudesh_project.filesystem_storage.id
}
