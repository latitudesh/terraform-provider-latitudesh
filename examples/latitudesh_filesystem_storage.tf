resource "latitudesh_filesystem_storage" "example" {
  project   = "proj_..."
  name      = "shared-data"
  region    = "CHI"
  protocols = ["nfs4"]
}
