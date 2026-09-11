resource "latitudesh_public_network" "network" {
  project = "proj_..."
  site    = "CHI"
  size    = 28
}

output "network_ipv4" {
  value = latitudesh_public_network.network.ipv4
}

data "latitudesh_public_network" "by_id" {
  id = latitudesh_public_network.network.id
}

data "latitudesh_public_network" "by_project_and_site" {
  project = "proj_..."
  site    = "CHI"
}
