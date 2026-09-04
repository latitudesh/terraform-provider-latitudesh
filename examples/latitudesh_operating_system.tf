data "latitudesh_operating_system" "ubuntu" {
  slug = "ubuntu_24_04_x64_lts"
}

output "operating_system_provisionable_on" {
  value = data.latitudesh_operating_system.ubuntu.provisionable_on
}

# `project` is inherited from the provider block:
#   provider "latitudesh" { project = "<project id or slug>" }
resource "latitudesh_server" "web" {
  hostname         = "web-01"
  plan             = "c3-small-x86"
  site             = "SAO2"
  operating_system = data.latitudesh_operating_system.ubuntu.slug
}
