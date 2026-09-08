resource "latitudesh_public_network" "network" {
  project = "proj_..."
  site    = "SAO2"
  size    = 28
}

output "network_ipv4" {
  value = latitudesh_public_network.network.ipv4
}

data "latitudesh_public_network" "by_id" {
  id = "pnet_..."
}

data "latitudesh_public_network" "by_project_and_site" {
  project = "proj_..."
  site    = "SAO2"
}
