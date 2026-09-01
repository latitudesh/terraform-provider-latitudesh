resource "latitudesh_object_storage" "backups" {
  project = "proj_..."
  name    = "app-backups"
  region  = "SAO2"

  versioning    = true
  storage_class = "standard"
}

output "backups_endpoint" {
  value = latitudesh_object_storage.backups.endpoint
}

data "latitudesh_object_storage" "by_id" {
  id = "bucket_..."
}

data "latitudesh_object_storage" "by_name" {
  name    = "app-backups"
  project = "proj_..."
}
