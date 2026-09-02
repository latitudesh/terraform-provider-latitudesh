resource "latitudesh_object_storage" "backups" {
  project = "proj_..."
  name    = "app-backups"
  region  = "ASH"

  versioning    = true
  storage_class = "standard"
}

output "backups_endpoint" {
  value = latitudesh_object_storage.backups.endpoint
}

data "latitudesh_object_storage" "by_id" {
  id = "bkt_..."
}

data "latitudesh_object_storage" "by_name" {
  name    = "app-backups"
  project = "proj_..."
}
